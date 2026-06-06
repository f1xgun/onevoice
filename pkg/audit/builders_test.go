package audit

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// fakeTx is a minimal pgx.Tx that only implements Exec — every other
// method panics with "fakeTx: not implemented". The tx-aware audit
// builders only call Exec, so panic-on-other-methods is a green signal
// that the implementation didn't drift to use a different method.
type fakeTx struct {
	pgx.Tx // embedded interface — all unused methods panic with nil deref.

	mu         sync.Mutex
	execCalled bool
	execSQL    string
	execArgs   []interface{}
	execErr    error
}

func (f *fakeTx) Exec(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execCalled = true
	f.execSQL = sql
	f.execArgs = args
	return pgconn.CommandTag{}, f.execErr
}

// captureLogger captures the last Entry passed to Log or LogSync so
// per-builder tests can assert its shape without touching the database.
// syncErr is returned verbatim by LogSync to exercise the fail-closed path.
type captureLogger struct {
	mu        sync.Mutex
	last      Entry
	lastSync  Entry
	syncCalls int
	syncErr   error
}

func (c *captureLogger) Log(_ context.Context, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = e
}

func (c *captureLogger) LogSync(_ context.Context, e Entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSync = e
	c.syncCalls++
	return c.syncErr
}

func TestBuilder_LogRoleGranted(t *testing.T) {
	c := &captureLogger{}
	biz, actor, target, newRole := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	old := uuid.New()
	LogRoleGranted(context.Background(), c, biz, actor, target, newRole, &old)
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionRoleGranted, c.last.Action)
	require.Equal(t, "role", c.last.Resource)
	require.NotNil(t, c.last.BusinessID)
	require.Equal(t, biz, *c.last.BusinessID)
	require.NotNil(t, c.last.UserID)
	require.Equal(t, actor, *c.last.UserID)
	var d RoleGrantedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, target, d.TargetUserID)
	require.NotNil(t, d.OldRoleID)
	require.Equal(t, old, *d.OldRoleID)
	require.Equal(t, newRole, d.NewRoleID)
}

func TestBuilder_LogLoginFailed_NoUserID(t *testing.T) {
	c := &captureLogger{}
	LogLoginFailed(context.Background(), c, "a@b.c", "1.2.3.4", "ua", "invalid_credentials")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionLoginFailed, c.last.Action)
	require.Nil(t, c.last.UserID, "LogLoginFailed must leave UserID nil")
	require.Nil(t, c.last.BusinessID)
	var d LoginFailedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, "a@b.c", d.AttemptedEmail)
	require.Equal(t, "invalid_credentials", d.Reason)
}

func TestBuilder_LogIntegrationTokenRotated_NoActor(t *testing.T) {
	c := &captureLogger{}
	biz, integ := uuid.New(), uuid.New()
	LogIntegrationTokenRotated(context.Background(), c, biz, integ, "telegram")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionIntegrationTokenRotated, c.last.Action)
	require.Equal(t, "integration", c.last.Resource)
	require.NotNil(t, c.last.BusinessID)
	require.Nil(t, c.last.UserID, "background token rotation has no actor")
	var d IntegrationTokenRotatedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, "telegram", d.Platform)
	require.Equal(t, integ, d.IntegrationID)
}

func TestBuilder_LogInvitationCreated_NoToken(t *testing.T) {
	c := &captureLogger{}
	biz, actor, inv, role := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	expires := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	LogInvitationCreated(context.Background(), c, biz, actor, inv, role, expires)
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionInvitationCreated, c.last.Action)
	require.NotContains(t, string(c.last.Details), "token")
	require.NotContains(t, string(c.last.Details), "secret")
}

func TestBuilder_LogProjectDeleted_BlastRadius(t *testing.T) {
	c := &captureLogger{}
	biz, actor, proj := uuid.New(), uuid.New(), uuid.New()
	LogProjectDeleted(context.Background(), c, biz, actor, proj, "old project", 42)
	c.mu.Lock()
	defer c.mu.Unlock()
	var d ProjectDeletedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, 42, d.DeletedConversations)
	require.Equal(t, "old project", d.Name)
}

// TestBuilder_AllBuildersExist_smoke calls every builder with placeholder
// args. The test fails to compile if any signature changes, providing a
// cheap regression guard for the 21-builder surface.
func TestBuilder_AllBuildersExist_smoke(_ *testing.T) {
	c := &captureLogger{}
	ctx := context.Background()
	u := uuid.New()
	LogRoleGranted(ctx, c, u, u, u, u, nil)
	LogMemberRemoved(ctx, c, u, u, u, false)
	LogRoleCreated(ctx, c, u, u, u, "x", []string{"a.b"})
	LogRoleUpdated(ctx, c, u, u, u, "x", []string{"a.b"})
	LogRoleDeleted(ctx, c, u, u, u, "x", nil, 0)
	LogInvitationCreated(ctx, c, u, u, u, u, time.Now())
	LogInvitationRevoked(ctx, c, u, u, u)
	LogInvitationAccepted(ctx, c, u, u, u, u)
	LogLoginSuccess(ctx, c, u, "ip", "ua")
	LogLoginFailed(ctx, c, "e", "ip", "ua", "r")
	LogLogout(ctx, c, u)
	LogPasswordChanged(ctx, c, u, "ip", "ua")
	LogUserRegistered(ctx, c, u, "e", "ip", "ua")
	LogIntegrationConnected(ctx, c, u, u, u, "tg", "ext", "1.2.3.4", "ua", "bot_token")
	LogIntegrationDisconnected(ctx, c, u, u, u, "tg")
	LogIntegrationTokenRotated(ctx, c, u, u, "tg")
	LogBusinessCreated(ctx, c, u, u, "n")
	LogBusinessUpdated(ctx, c, u, u)
	LogProjectCreated(ctx, c, u, u, u, "n")
	LogProjectUpdated(ctx, c, u, u, u)
	LogProjectDeleted(ctx, c, u, u, u, "n", 3)
}

// ---- consent builders -----------------------------

// TestLogConsentRecordedTx_InsertsRow asserts the tx-aware builder issues
// one INSERT against audit_logs with the correct action + Details JSON.
func TestLogConsentRecordedTx_InsertsRow(t *testing.T) {
	tx := &fakeTx{}
	userID := uuid.New()
	purposes := []string{"tos", "privacy", "pdn"}
	err := LogConsentRecordedTx(context.Background(), tx, userID, purposes, "v1.0", "sha-abc", "1.2.3.4", "UA-string")
	require.NoError(t, err)
	tx.mu.Lock()
	defer tx.mu.Unlock()
	require.True(t, tx.execCalled, "Exec must be called")
	require.Contains(t, tx.execSQL, "INSERT INTO audit_logs")
	require.Len(t, tx.execArgs, 5, "INSERT must bind 5 args (userID, email, action, resource, details)")
	require.Equal(t, userID, tx.execArgs[0])
	require.Equal(t, "", tx.execArgs[1], "user_email_at_event empty for Register-tx writes")
	require.Equal(t, ActionConsentRecorded, tx.execArgs[2])
	require.Equal(t, "user", tx.execArgs[3])
	detailsBytes, ok := tx.execArgs[4].([]byte)
	require.True(t, ok, "details arg must be []byte")
	var d ConsentRecordedDetails
	require.NoError(t, json.Unmarshal(detailsBytes, &d))
	require.Equal(t, purposes, d.Purposes)
	require.Equal(t, "v1.0", d.PolicyVersion)
	require.Equal(t, "sha-abc", d.PolicySHA256)
	require.Equal(t, "1.2.3.4", d.IP)
	require.Equal(t, "UA-string", d.UserAgent)
}

// TestLogConsentReconsentedTx_InsertsRow asserts the tx-aware reconsented
// builder writes the consent_reconsented action with from→to version data.
func TestLogConsentReconsentedTx_InsertsRow(t *testing.T) {
	tx := &fakeTx{}
	userID := uuid.New()
	err := LogConsentReconsentedTx(context.Background(), tx, userID, []string{"tos", "privacy", "pdn"}, "pre-v22", "v1.0", "1.2.3.4", "UA")
	require.NoError(t, err)
	tx.mu.Lock()
	defer tx.mu.Unlock()
	require.True(t, tx.execCalled)
	require.Equal(t, ActionConsentReconsented, tx.execArgs[2])
	var d ConsentReconsentedDetails
	require.NoError(t, json.Unmarshal(tx.execArgs[4].([]byte), &d))
	require.Equal(t, "pre-v22", d.FromVersion)
	require.Equal(t, "v1.0", d.ToVersion)
	require.Equal(t, "1.2.3.4", d.IP)
}

// TestLogConsentWithdrawnTx_InsertsRow asserts the tx-aware withdrawal
// builder writes the consent_withdrawn action with Purpose="pdn".
func TestLogConsentWithdrawnTx_InsertsRow(t *testing.T) {
	tx := &fakeTx{}
	userID := uuid.New()
	err := LogConsentWithdrawnTx(context.Background(), tx, userID, "pdn", "1.2.3.4", "UA")
	require.NoError(t, err)
	tx.mu.Lock()
	defer tx.mu.Unlock()
	require.True(t, tx.execCalled)
	require.Equal(t, ActionConsentWithdrawn, tx.execArgs[2])
	var d ConsentWithdrawnDetails
	require.NoError(t, json.Unmarshal(tx.execArgs[4].([]byte), &d))
	require.Equal(t, "pdn", d.Purpose)
	require.Equal(t, "1.2.3.4", d.IP)
	require.Equal(t, "UA", d.UserAgent)
}

// TestLogConsentReconsentRequired_FireAndForget asserts the fire-and-
// forget builder pushes one Entry with the expected action + policies
// slice + currentVersion.
func TestLogConsentReconsentRequired_FireAndForget(t *testing.T) {
	c := &captureLogger{}
	userID := uuid.New()
	LogConsentReconsentRequired(context.Background(), c, userID, []string{"tos", "privacy"}, "v1.0")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionConsentReconsentRequired, c.last.Action)
	require.Equal(t, "user", c.last.Resource)
	require.NotNil(t, c.last.UserID)
	require.Equal(t, userID, *c.last.UserID)
	var d ConsentReconsentRequiredDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, []string{"tos", "privacy"}, d.Policies)
	require.Equal(t, "v1.0", d.CurrentVersion)
}

// TestLogConsentPolicyVersionBumped_SystemEvent asserts the policy-bump
// builder leaves UserID nil (system event, no actor) and resource="policy".
func TestLogConsentPolicyVersionBumped_SystemEvent(t *testing.T) {
	c := &captureLogger{}
	LogConsentPolicyVersionBumped(context.Background(), c, "pdn", "v1.0", "v1.1", "sha-new")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionConsentPolicyVersionBumped, c.last.Action)
	require.Equal(t, "policy", c.last.Resource)
	require.Nil(t, c.last.UserID, "policy bump is a system event with no actor")
	var d ConsentPolicyVersionBumpedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, "pdn", d.Slug)
	require.Equal(t, "v1.0", d.FromVersion)
	require.Equal(t, "v1.1", d.ToVersion)
	require.Equal(t, "sha-new", d.SHA256)
}

// ---- integration sec-hardening builders ----------------------------------

func TestLogIntegrationConnected_CarriesMetadata(t *testing.T) {
	c := &captureLogger{}
	biz, actor, integ := uuid.New(), uuid.New(), uuid.New()
	LogIntegrationConnected(context.Background(), c, biz, actor, integ, "yandex_business", "org-1", "203.0.113.7", "Mozilla/5.0", "cookie_header")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, ActionIntegrationConnected, c.last.Action)
	require.Equal(t, "integration", c.last.Resource)
	require.NotNil(t, c.last.BusinessID)
	require.Equal(t, biz, *c.last.BusinessID)
	require.NotNil(t, c.last.UserID)
	require.Equal(t, actor, *c.last.UserID)
	var d IntegrationConnectedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, integ, d.IntegrationID)
	require.Equal(t, "yandex_business", d.Platform)
	require.Equal(t, "org-1", d.ExternalID)
	require.Equal(t, "203.0.113.7", d.ActorIP)
	require.Equal(t, "Mozilla/5.0", d.UserAgent)
	require.Equal(t, "cookie_header", d.ParsedFormat)
}

func TestLogTokenDecryptedSync_PropagatesError(t *testing.T) {
	sentinel := errors.New("insert failed")
	c := &captureLogger{syncErr: sentinel}
	biz, integ := uuid.New(), uuid.New()
	err := LogTokenDecryptedSync(context.Background(), c, biz, integ, "telegram", "agent-telegram", "corr-9", "telegram_notify")
	require.ErrorIs(t, err, sentinel)
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, 1, c.syncCalls, "LogTokenDecryptedSync must call LogSync exactly once")
	require.Equal(t, ActionIntegrationTokenDecrypted, c.lastSync.Action)
	require.Equal(t, "integration", c.lastSync.Resource)
	require.NotNil(t, c.lastSync.BusinessID)
	require.Equal(t, biz, *c.lastSync.BusinessID)
	var d TokenDecryptedDetails
	require.NoError(t, json.Unmarshal(c.lastSync.Details, &d))
	require.Equal(t, integ, d.IntegrationID)
	require.Equal(t, "telegram", d.Platform)
	require.Equal(t, "agent-telegram", d.CallerService)
	require.Equal(t, "corr-9", d.CorrelationID)
	require.Equal(t, "telegram_notify", d.Reason)
}

func TestLogTokenDecryptedSync_SuccessReturnsNil(t *testing.T) {
	c := &captureLogger{}
	err := LogTokenDecryptedSync(context.Background(), c, uuid.New(), uuid.New(), "vk", "agent-vk", "", "vk_post")
	require.NoError(t, err)
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, 1, c.syncCalls)
}

func TestLogIntegrationDeleted_FireAndForget(t *testing.T) {
	c := &captureLogger{}
	biz, actor, integ := uuid.New(), uuid.New(), uuid.New()
	LogIntegrationDeleted(context.Background(), c, biz, actor, integ, "vk", "club123")
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Equal(t, 0, c.syncCalls, "LogIntegrationDeleted must be fire-and-forget, not sync")
	require.Equal(t, ActionIntegrationDeleted, c.last.Action)
	require.Equal(t, "integration", c.last.Resource)
	require.NotNil(t, c.last.BusinessID)
	require.Equal(t, biz, *c.last.BusinessID)
	require.NotNil(t, c.last.UserID)
	require.Equal(t, actor, *c.last.UserID)
	var d IntegrationDeletedDetails
	require.NoError(t, json.Unmarshal(c.last.Details, &d))
	require.Equal(t, integ, d.IntegrationID)
	require.Equal(t, "vk", d.Platform)
	require.Equal(t, "club123", d.ExternalID)
}
