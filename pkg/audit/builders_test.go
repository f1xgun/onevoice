package audit

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// captureLogger captures the last Entry passed to Log so per-builder
// tests can assert its shape without touching the database.
type captureLogger struct {
	mu   sync.Mutex
	last Entry
}

func (c *captureLogger) Log(_ context.Context, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = e
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
	require.Nil(t, c.last.UserID, "LogLoginFailed must leave UserID nil (D-31)")
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
	LogIntegrationConnected(ctx, c, u, u, u, "tg", "ext")
	LogIntegrationDisconnected(ctx, c, u, u, u, "tg")
	LogIntegrationTokenRotated(ctx, c, u, u, "tg")
	LogBusinessCreated(ctx, c, u, u, "n")
	LogBusinessUpdated(ctx, c, u, u)
	LogProjectCreated(ctx, c, u, u, u, "n")
	LogProjectUpdated(ctx, c, u, u, u)
	LogProjectDeleted(ctx, c, u, u, u, "n", 3)
}
