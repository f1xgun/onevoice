package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// mustMarshal serializes a Details struct, emitting "{}" on failure
// because a marshal error here is a developer bug (unmarshalable field),
// not a runtime fault — must not panic the request path.
//
// Tx-aware builders deliberately do NOT use this helper: they propagate
// marshal failure as an error so the surrounding transaction rolls back.
func mustMarshal(d interface{}) json.RawMessage {
	b, err := json.Marshal(d)
	if err != nil {
		slog.Error("audit: marshal failed", "error", err)
		return json.RawMessage("{}")
	}
	return b
}

// Builder signatures put ctx first (revive context-as-argument) and
// Logger second so call sites read naturally:
// `audit.LogRoleGranted(ctx, logger, biz, actor, ...)`.
//
// See docs/pkg/audit-builders.md for the full discipline contract.

// ---- rbac builders ------------------------------------------------------

// LogRoleGranted records a member's role change. oldRoleID may be nil for
// brand-new memberships.
func LogRoleGranted(ctx context.Context, l Logger, businessID, actorID, targetUserID, newRoleID uuid.UUID, oldRoleID *uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionRoleGranted,
		Resource:   "role",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(RoleGrantedDetails{TargetUserID: targetUserID, OldRoleID: oldRoleID, NewRoleID: newRoleID}),
	})
}

// LogMemberRemoved records a member being removed; selfRemoval=true
// distinguishes "left org" from "kicked out".
func LogMemberRemoved(ctx context.Context, l Logger, businessID, actorID, targetUserID uuid.UUID, selfRemoval bool) {
	l.Log(ctx, Entry{
		Action:     ActionMemberRemoved,
		Resource:   "member",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(MemberRemovedDetails{TargetUserID: targetUserID, SelfRemoval: selfRemoval}),
	})
}

// LogRoleCreated records a custom role being created with its initial
// permission set.
func LogRoleCreated(ctx context.Context, l Logger, businessID, actorID, roleID uuid.UUID, roleName string, perms []string) {
	l.Log(ctx, Entry{
		Action:     ActionRoleCreated,
		Resource:   "role",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(RoleCreatedDetails{RoleID: roleID, RoleName: roleName, Permissions: perms}),
	})
}

// LogRoleUpdated records a custom role's post-update permission set.
func LogRoleUpdated(ctx context.Context, l Logger, businessID, actorID, roleID uuid.UUID, newName string, perms []string) {
	l.Log(ctx, Entry{
		Action:     ActionRoleUpdated,
		Resource:   "role",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(RoleUpdatedDetails{RoleID: roleID, NewName: newName, Permissions: perms}),
	})
}

// LogRoleDeleted records blast radius (affectedUsers) and where members
// were reassigned (reassignedTo may be nil if zero members held the role).
func LogRoleDeleted(ctx context.Context, l Logger, businessID, actorID, roleID uuid.UUID, roleName string, reassignedTo *uuid.UUID, affectedUsers int) {
	l.Log(ctx, Entry{
		Action:     ActionRoleDeleted,
		Resource:   "role",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(RoleDeletedDetails{RoleID: roleID, RoleName: roleName, ReassignedTo: reassignedTo, AffectedUsers: affectedUsers}),
	})
}

// LogInvitationCreated records an invitation being issued. NO token or
// token_hash in details.
func LogInvitationCreated(ctx context.Context, l Logger, businessID, actorID, invitationID, roleID uuid.UUID, expiresAt time.Time) {
	l.Log(ctx, Entry{
		Action:     ActionInvitationCreated,
		Resource:   "invitation",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(InvitationCreatedDetails{InvitationID: invitationID, RoleID: roleID, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}),
	})
}

// LogInvitationRevoked records an invitation being revoked.
func LogInvitationRevoked(ctx context.Context, l Logger, businessID, actorID, invitationID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionInvitationRevoked,
		Resource:   "invitation",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(InvitationRevokedDetails{InvitationID: invitationID}),
	})
}

// LogInvitationAccepted records an invitation being accepted by a user.
// accepterUserID is the user accepting (== granted member).
func LogInvitationAccepted(ctx context.Context, l Logger, businessID, accepterUserID, invitationID, grantedRoleID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionInvitationAccepted,
		Resource:   "invitation",
		BusinessID: &businessID,
		UserID:     &accepterUserID,
		Details:    mustMarshal(InvitationAcceptedDetails{InvitationID: invitationID, GrantedRoleID: grantedRoleID}),
	})
}

// ---- auth builders ------------------------------------------------------

// LogLoginSuccess records a successful login. business_id = nil (auth
// events are system-wide, not business-scoped).
func LogLoginSuccess(ctx context.Context, l Logger, userID uuid.UUID, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionLoginSuccess,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(LoginSuccessDetails{IP: ip, UserAgent: userAgent}),
	})
}

// LogLoginFailed records a failed login. UserID intentionally nil;
// attemptedEmail goes in details for brute-force analysis.
func LogLoginFailed(ctx context.Context, l Logger, attemptedEmail, ip, userAgent, reason string) {
	l.Log(ctx, Entry{
		Action:   ActionLoginFailed,
		Resource: "user",
		Details:  mustMarshal(LoginFailedDetails{AttemptedEmail: attemptedEmail, IP: ip, UserAgent: userAgent, Reason: reason}),
	})
}

// LogLogout records a logout. business_id = nil.
func LogLogout(ctx context.Context, l Logger, userID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:   ActionLogout,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(LogoutDetails{}),
	})
}

// LogPasswordChanged records a password change. NO old or new password
// in details.
func LogPasswordChanged(ctx context.Context, l Logger, userID uuid.UUID, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionPasswordChanged,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(PasswordChangedDetails{IP: ip, UserAgent: userAgent}),
	})
}

// LogUserRegistered records a new account being created (post auto-login).
func LogUserRegistered(ctx context.Context, l Logger, userID uuid.UUID, email, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionUserRegistered,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(UserRegisteredDetails{Email: email, IP: ip, UserAgent: userAgent}),
	})
}

// LogEmailVerified records a successful email verification (POST
// /auth/verify-email/confirm). No old/new state in details — the
// AuditLog.UserEmailAtEvent snapshot already captures the address that
// was just verified.
func LogEmailVerified(ctx context.Context, l Logger, userID uuid.UUID, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionEmailVerified,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(EmailVerifiedDetails{IP: ip, UserAgent: userAgent}),
	})
}

// LogEmailChangedBeforeVerify records PATCH /auth/email-before-verify.
// Captures both the OLD and NEW email so the forensic trail shows email
// churn during the unverified window.
func LogEmailChangedBeforeVerify(ctx context.Context, l Logger, userID uuid.UUID, oldEmail, newEmail, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionEmailChangedBeforeVerify,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(EmailChangedBeforeVerifyDetails{OldEmail: oldEmail, NewEmail: newEmail, IP: ip, UserAgent: userAgent}),
	})
}

// LogConsentRecorded is the legacy fire-and-forget Register-flow consent
// recorder. New paths use LogConsentRecordedTx.
//
// See docs/pkg/audit-builders.md.
func LogConsentRecorded(ctx context.Context, l Logger, userID uuid.UUID, purpose, policyVersion string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentRecorded,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(ConsentRecordedDetails{Purpose: purpose, PolicyVersion: policyVersion}),
	})
}

// ---- consent builders ---------------------------------------------------

// LogConsentRecordedTx records the Register-flow consent INSERT inside
// the caller's pgx.Tx so the audit row commits atomically with the
// user_consents UPSERTs (152-ФЗ forensic invariant).
//
// See docs/pkg/audit-builders.md for tx-aware rationale.
func LogConsentRecordedTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purposes []string, policyVersion, policySHA256, ip, userAgent string) error {
	d, err := json.Marshal(ConsentRecordedDetails{
		Purposes:      purposes,
		PolicyVersion: policyVersion,
		PolicySHA256:  policySHA256,
		IP:            ip,
		UserAgent:     userAgent,
	})
	if err != nil {
		return fmt.Errorf("audit: marshal consent_recorded details: %w", err)
	}
	const q = `INSERT INTO audit_logs (id, user_id, user_email_at_event, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())`
	if _, err := tx.Exec(ctx, q, userID, "", ActionConsentRecorded, "user", d); err != nil {
		return fmt.Errorf("audit: consent_recorded insert: %w", err)
	}
	return nil
}

// LogConsentReconsentedTx records POST /auth/consents inside the same tx
// as the UPSERTs. fromVersion is the user's prior version; toVersion is
// the build's currentVersion.
func LogConsentReconsentedTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purposes []string, fromVersion, toVersion, ip, userAgent string) error {
	d, err := json.Marshal(ConsentReconsentedDetails{
		Purposes:    purposes,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		IP:          ip,
		UserAgent:   userAgent,
	})
	if err != nil {
		return fmt.Errorf("audit: marshal consent_reconsented details: %w", err)
	}
	const q = `INSERT INTO audit_logs (id, user_id, user_email_at_event, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())`
	if _, err := tx.Exec(ctx, q, userID, "", ActionConsentReconsented, "user", d); err != nil {
		return fmt.Errorf("audit: consent_reconsented insert: %w", err)
	}
	return nil
}

// LogConsentWithdrawnTx records POST /users/me/consents/pdn/withdraw
// inside the user_consents.withdrawn_at UPDATE tx.
func LogConsentWithdrawnTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose, ip, userAgent string) error {
	d, err := json.Marshal(ConsentWithdrawnDetails{
		Purpose:   purpose,
		IP:        ip,
		UserAgent: userAgent,
	})
	if err != nil {
		return fmt.Errorf("audit: marshal consent_withdrawn details: %w", err)
	}
	const q = `INSERT INTO audit_logs (id, user_id, user_email_at_event, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())`
	if _, err := tx.Exec(ctx, q, userID, "", ActionConsentWithdrawn, "user", d); err != nil {
		return fmt.Errorf("audit: consent_withdrawn insert: %w", err)
	}
	return nil
}

// LogConsentReconsentRequired is fire-and-forget — emitted from /auth/me
// when DiffAgainstCurrent reports stale policies. Read-only path, no tx.
func LogConsentReconsentRequired(ctx context.Context, l Logger, userID uuid.UUID, policies []string, currentVersion string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentReconsentRequired,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(ConsentReconsentRequiredDetails{Policies: policies, CurrentVersion: currentVersion}),
	})
}

// LogConsentPolicyVersionBumped is a fire-and-forget system event with
// no UserID. Emitted once per environment when the API detects a new
// build's currentVersion exceeds the most-recent recorded one.
func LogConsentPolicyVersionBumped(ctx context.Context, l Logger, slug, fromVersion, toVersion, sha256 string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentPolicyVersionBumped,
		Resource: "policy",
		UserID:   nil,
		Details:  mustMarshal(ConsentPolicyVersionBumpedDetails{Slug: slug, FromVersion: fromVersion, ToVersion: toVersion, SHA256: sha256}),
	})
}

// ---- account.* (deletion) ----------------------------------------------

// LogDeletionRequested records account.deletion_requested on soft-delete
// via DELETE /users/me. orphanedBusinessIDs is currently always empty
// (handler returns 409 for sole-owner) but is recorded for
// forward-compatibility with ownership-transfer.
func LogDeletionRequested(ctx context.Context, l Logger, userID uuid.UUID, ip, ua string, orphanedBusinessIDs []uuid.UUID) {
	l.Log(ctx, Entry{
		Action:   ActionDeletionRequested,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(DeletionRequestedDetails{IP: ip, UserAgent: ua, BusinessesOrphaned: orphanedBusinessIDs}),
	})
}

// LogDeletionCanceled records account.deletion_canceled on POST
// /users/me/restore within the 30-day grace window.
func LogDeletionCanceled(ctx context.Context, l Logger, userID uuid.UUID, ip, ua string) {
	l.Log(ctx, Entry{
		Action:   ActionDeletionCanceled,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(DeletionCanceledDetails{IP: ip, UserAgent: ua}),
	})
}

// LogSoleOwnerBlocked records account.sole_owner_blocked when DELETE
// /users/me is rejected because the user is the sole OWNER of one or
// more businesses.
func LogSoleOwnerBlocked(ctx context.Context, l Logger, userID uuid.UUID, ip, ua string, soleOwnerBusinessIDs []uuid.UUID) {
	l.Log(ctx, Entry{
		Action:   ActionSoleOwnerBlocked,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(SoleOwnerBlockedDetails{IP: ip, UserAgent: ua, BusinessIDs: soleOwnerBusinessIDs}),
	})
}

// LogUserSelfDeletedTx is INTENTIONALLY called WITHIN the HardDelete PG
// tx — the audit insert is atomic with the user row deletion. The audit
// row MUST land before the DELETE so the FK SET NULL has somewhere to
// land + user_email_at_event preserves the email for 152-ФЗ forensic
// queries.
//
// See docs/pkg/audit-builders.md.
func LogUserSelfDeletedTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, originalEmail string) error {
	const q = `INSERT INTO audit_logs (id, user_id, user_email_at_event, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())`
	if _, err := tx.Exec(ctx, q, userID, originalEmail, ActionUserSelfDeleted, "user", []byte(`{}`)); err != nil {
		return fmt.Errorf("audit: user_self_deleted insert: %w", err)
	}
	return nil
}

// ---- integration builders -----------------------------------------------

// LogIntegrationConnected records a new integration. Details carry only
// platform + external_id plus forensic provenance (actorIP, userAgent,
// parsedFormat) — never token material.
func LogIntegrationConnected(ctx context.Context, l Logger, businessID, actorID, integrationID uuid.UUID, platform, externalID, actorIP, userAgent, parsedFormat string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationConnected,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details: mustMarshal(IntegrationConnectedDetails{
			IntegrationID: integrationID,
			Platform:      platform,
			ExternalID:    externalID,
			ActorIP:       actorIP,
			UserAgent:     userAgent,
			ParsedFormat:  parsedFormat,
		}),
	})
}

// LogIntegrationDisconnected records an integration being removed.
// Capture platform BEFORE the row is deleted.
func LogIntegrationDisconnected(ctx context.Context, l Logger, businessID, actorID, integrationID uuid.UUID, platform string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationDisconnected,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(IntegrationDisconnectedDetails{IntegrationID: integrationID, Platform: platform}),
	})
}

// LogIntegrationTokenRotated records a background token refresh. UserID
// intentionally nil — system event with no user actor.
func LogIntegrationTokenRotated(ctx context.Context, l Logger, businessID, integrationID uuid.UUID, platform string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationTokenRotated,
		Resource:   "integration",
		BusinessID: &businessID,
		Details:    mustMarshal(IntegrationTokenRotatedDetails{IntegrationID: integrationID, Platform: platform}),
	})
}

// LogTokenDecryptedSync records a single token decryption synchronously and
// fail-closed: the returned error (wrapping the repository Insert error) MUST
// abort the caller so a token is never released without a forensic row.
// keyVersion is the KMS key version active at decrypt time; 0 for legacy rows
// that use the flat AES path (WrappedDEK IS NULL).
func LogTokenDecryptedSync(ctx context.Context, l Logger, businessID, integrationID uuid.UUID, platform, callerService, correlationID, reason string, keyVersion int16) error {
	if err := l.LogSync(ctx, Entry{
		Action:     ActionIntegrationTokenDecrypted,
		Resource:   "integration",
		BusinessID: &businessID,
		Details: mustMarshal(TokenDecryptedDetails{
			IntegrationID: integrationID,
			Platform:      platform,
			CallerService: callerService,
			CorrelationID: correlationID,
			Reason:        reason,
			KeyVersion:    keyVersion,
		}),
	}); err != nil {
		return fmt.Errorf("audit token_decrypted: %w", err)
	}
	return nil
}

// LogIntegrationDeleted records a soft-delete snapshot fire-and-forget. The
// details survive even after the integration row is hard-purged.
func LogIntegrationDeleted(ctx context.Context, l Logger, businessID, actorID, integrationID uuid.UUID, platform, externalID string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationDeleted,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details: mustMarshal(IntegrationDeletedDetails{
			IntegrationID: integrationID,
			Platform:      platform,
			ExternalID:    externalID,
		}),
	})
}

// LogIntegrationMetadataUpdated records a metadata-only heal. UserID
// intentionally nil — system/agent-initiated event with no user actor.
// updatedKeys logs the metadata KEYS ONLY, never the values.
func LogIntegrationMetadataUpdated(ctx context.Context, l Logger, businessID, integrationID uuid.UUID, platform string, updatedKeys []string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationMetadataUpdated,
		Resource:   "integration",
		BusinessID: &businessID,
		Details: mustMarshal(IntegrationMetadataUpdatedDetails{
			IntegrationID: integrationID,
			Platform:      platform,
			UpdatedKeys:   updatedKeys,
		}),
	})
}

// LogIntegrationExternalIDUpdated records an external_id heal. UserID
// intentionally nil — system/agent-initiated event with no user actor.
func LogIntegrationExternalIDUpdated(ctx context.Context, l Logger, businessID, integrationID uuid.UUID, platform, oldExternalID, newExternalID string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationExternalIDUpdated,
		Resource:   "integration",
		BusinessID: &businessID,
		Details: mustMarshal(IntegrationExternalIDUpdatedDetails{
			IntegrationID: integrationID,
			Platform:      platform,
			OldExternalID: oldExternalID,
			NewExternalID: newExternalID,
		}),
	})
}

// LogIntegrationTokenExpired records a status flip to token_expired. UserID
// intentionally nil — system/agent-initiated event with no user actor. There
// is no single integrationID because an empty externalID may flip several rows.
func LogIntegrationTokenExpired(ctx context.Context, l Logger, businessID uuid.UUID, platform, externalID string, rowsAffected int) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationTokenExpired,
		Resource:   "integration",
		BusinessID: &businessID,
		Details: mustMarshal(IntegrationTokenExpiredDetails{
			Platform:     platform,
			ExternalID:   externalID,
			RowsAffected: rowsAffected,
		}),
	})
}

// ---- business builders --------------------------------------------------

// LogBusinessCreated records a new business + owner.
func LogBusinessCreated(ctx context.Context, l Logger, businessID, ownerUserID uuid.UUID, name string) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessCreated,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &ownerUserID,
		Details:    mustMarshal(BusinessCreatedDetails{Name: name}),
	})
}

// LogBusinessUpdated records a business update. v1 ships without
// per-field diff.
func LogBusinessUpdated(ctx context.Context, l Logger, businessID, actorID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessUpdated,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(BusinessUpdatedDetails{}),
	})
}

// LogBusinessDeletionRequested records business.deletion_requested on
// soft-delete via DELETE /businesses/{id}.
func LogBusinessDeletionRequested(ctx context.Context, l Logger, businessID, actorID uuid.UUID, ip, ua string) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessDeletionRequested,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(BusinessDeletionRequestedDetails{IP: ip, UserAgent: ua}),
	})
}

// LogBusinessDeletionCanceled records business.deletion_canceled on POST
// /businesses/{id}/restore within the 30-day grace window.
func LogBusinessDeletionCanceled(ctx context.Context, l Logger, businessID, actorID uuid.UUID, ip, ua string) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessDeletionCanceled,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(BusinessDeletionCanceledDetails{IP: ip, UserAgent: ua}),
	})
}

// LogBusinessNotOwnerBlocked records business.not_owner_blocked when a
// delete/restore attempt is rejected because the actor is not an OWNER.
func LogBusinessNotOwnerBlocked(ctx context.Context, l Logger, businessID, actorID uuid.UUID, ip, ua string) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessNotOwnerBlocked,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(BusinessNotOwnerBlockedDetails{IP: ip, UserAgent: ua}),
	})
}

// LogBusinessSelfDeletedTx is INTENTIONALLY called WITHIN the HardDelete PG tx —
// the audit insert is atomic with the businesses-row deletion. The audit row
// MUST land before the DELETE so the FK SET NULL has somewhere to land + the
// name snapshot survives in details for forensic queries.
func LogBusinessSelfDeletedTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID, originalName string) error {
	const q = `INSERT INTO audit_logs (id, business_id, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, NOW())`
	details, err := json.Marshal(BusinessCreatedDetails{Name: originalName})
	if err != nil {
		return fmt.Errorf("audit: business_self_deleted marshal: %w", err)
	}
	if _, err := tx.Exec(ctx, q, businessID, ActionBusinessSelfDeleted, "business", details); err != nil {
		return fmt.Errorf("audit: business_self_deleted insert: %w", err)
	}
	return nil
}

// ---- project builders ---------------------------------------------------

// LogProjectCreated records a new project.
func LogProjectCreated(ctx context.Context, l Logger, businessID, actorID, projectID uuid.UUID, name string) {
	l.Log(ctx, Entry{
		Action:     ActionProjectCreated,
		Resource:   "project",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(ProjectCreatedDetails{ProjectID: projectID, Name: name}),
	})
}

// LogProjectUpdated records a project update.
func LogProjectUpdated(ctx context.Context, l Logger, businessID, actorID, projectID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionProjectUpdated,
		Resource:   "project",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(ProjectUpdatedDetails{ProjectID: projectID}),
	})
}

// LogProjectDeleted records a project deletion with blast radius
// (deletedConvs).
func LogProjectDeleted(ctx context.Context, l Logger, businessID, actorID, projectID uuid.UUID, name string, deletedConvs int) {
	l.Log(ctx, Entry{
		Action:     ActionProjectDeleted,
		Resource:   "project",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(ProjectDeletedDetails{ProjectID: projectID, Name: name, DeletedConversations: deletedConvs}),
	})
}

// ---- rpa builders -------------------------------------------------------

// LogRPAScopeViolation records a blocked navigation attempt from a Playwright
// browser context. Fire-and-forget; observability-grade event.
func LogRPAScopeViolation(ctx context.Context, l Logger, businessID uuid.UUID, hostname, attemptedURL string) {
	l.Log(ctx, Entry{
		Action:     ActionRPAScopeViolation,
		Resource:   "integration",
		BusinessID: &businessID,
		Details: mustMarshal(RPAScopeViolationDetails{
			Hostname:     hostname,
			AttemptedURL: attemptedURL,
			AllowedScope: "business.yandex.ru",
		}),
	})
}

// LogRPAMutation records a write the RPA worker landed on a third-party public
// listing (reply / post / photo / info / hours). action is one of the
// ActionRPA* mutation constants; actorID may be nil when the triggering user
// can't be resolved (the event is still attributable to businessID). target is
// a non-PII external identifier (e.g. review id) or "". Fire-and-forget.
func LogRPAMutation(ctx context.Context, l Logger, action string, businessID uuid.UUID, actorID *uuid.UUID, tool, platform, target string) {
	l.Log(ctx, Entry{
		Action:     action,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     actorID,
		Details:    mustMarshal(RPAMutationDetails{Tool: tool, Platform: platform, Target: target}),
	})
}

// LogPlatformMutation records a direct-API write the agents landed on a
// connected platform (Telegram/VK) via the owner's token — a published post, an
// owner DM, or a public review/comment reply dispatched through the chat turn.
// action is one of the ActionPlatform* constants; actorID may be nil when the
// triggering user can't be resolved (the event is still attributable to
// businessID). target is a non-PII external identifier (post id / review id) or
// "". This is the direct-API counterpart to LogRPAMutation. Fire-and-forget.
func LogPlatformMutation(ctx context.Context, l Logger, action string, businessID uuid.UUID, actorID *uuid.UUID, tool, platform, target string) {
	l.Log(ctx, Entry{
		Action:     action,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     actorID,
		Details:    mustMarshal(RPAMutationDetails{Tool: tool, Platform: platform, Target: target}),
	})
}

// ---- review builders ----------------------------------------------------

// LogReviewAutoReplied records the review-reply autopilot auto-publishing a
// positive drafted reply on a direct-API platform. UserID is intentionally nil:
// no user drove the individual reply, so the event is attributable to businessID
// alone (an automated system action). tool is the canonical reply tool, platform
// the listing provider, and target the review's non-PII external id. Details
// carry no review text, author name, or reply body. Fire-and-forget.
func LogReviewAutoReplied(ctx context.Context, l Logger, businessID uuid.UUID, platform, tool, target string) {
	l.Log(ctx, Entry{
		Action:     ActionReviewAutoReplied,
		Resource:   "review",
		BusinessID: &businessID,
		UserID:     nil,
		Details:    mustMarshal(RPAMutationDetails{Tool: tool, Platform: platform, Target: target}),
	})
}

// ---- hitl builders ------------------------------------------------------

// LogHITLApprovalResolved records an off-app HITL approval resolution (owner
// tapped Approve/Reject in the Telegram DM). ownerUserID is the verified owner
// who resolved it (goes on UserID so the row is attributable to a real actor);
// channel is the inbound plane ("telegram"); action is the batch-wide verdict
// ("approve" / "reject"); callCount is how many tool calls the batch held.
// Fire-and-forget — the resolve already succeeded, so a lost audit write must
// not roll it back; a terminal failure increments the audit failure metric.
func LogHITLApprovalResolved(ctx context.Context, l Logger, businessID, ownerUserID uuid.UUID, batchID, conversationID, channel, action string, callCount int) {
	l.Log(ctx, Entry{
		Action:     ActionHITLApprovalResolved,
		Resource:   "conversation",
		BusinessID: &businessID,
		UserID:     &ownerUserID,
		Details: mustMarshal(HITLApprovalResolvedDetails{
			BatchID:        batchID,
			ConversationID: conversationID,
			Channel:        channel,
			Action:         action,
			CallCount:      callCount,
		}),
	})
}
