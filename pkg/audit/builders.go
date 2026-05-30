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

// mustMarshal is the internal helper used by every builder. Failure means
// a Details struct is misconfigured at compile time (struct field cannot
// be marshaled), which is a developer bug not a runtime fault — we log
// and emit an empty object rather than panic in the request path.
func mustMarshal(d interface{}) json.RawMessage {
	b, err := json.Marshal(d)
	if err != nil {
		slog.Error("audit: marshal failed", "error", err)
		return json.RawMessage("{}")
	}
	return b
}

// Builder signatures put ctx first (Go idiom enforced by revive
// context-as-argument). Logger is the second parameter so call sites read
// `audit.LogRoleGranted(ctx, logger, biz, actor, ...)` — ctx is the
// invariant request-context, logger is the dependency.

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

// LogInvitationCreated records an invitation being issued. : NO token
// or token_hash in details.
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

// LogLoginSuccess records a successful login. business_id = nil
// (auth events are system-wide, not business-scoped).
func LogLoginSuccess(ctx context.Context, l Logger, userID uuid.UUID, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionLoginSuccess,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(LoginSuccessDetails{IP: ip, UserAgent: userAgent}),
	})
}

// LogLoginFailed — UserID intentionally nil. attemptedEmail is
// captured in details for brute-force analysis.
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

// LogPasswordChanged records a password change. NO old or new password in
// details.
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
// /auth/verify-email/confirm). /. No old/new state in
// details — the AuditLog.UserEmailAtEvent snapshot already captures the
// address that was just verified.
func LogEmailVerified(ctx context.Context, l Logger, userID uuid.UUID, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionEmailVerified,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(EmailVerifiedDetails{IP: ip, UserAgent: userAgent}),
	})
}

// LogEmailChangedBeforeVerify records PATCH /auth/email-before-verify.
// Captures both the OLD email (was: pre-change address) and the NEW email
// so the forensic trail shows email churn during the unverified window.
func LogEmailChangedBeforeVerify(ctx context.Context, l Logger, userID uuid.UUID, oldEmail, newEmail, ip, userAgent string) {
	l.Log(ctx, Entry{
		Action:   ActionEmailChangedBeforeVerify,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(EmailChangedBeforeVerifyDetails{OldEmail: oldEmail, NewEmail: newEmail, IP: ip, UserAgent: userAgent}),
	})
}

// LogConsentRecorded records the user_consents INSERT that runs alongside
// Register. /. EXTENDS this with the
// tx-aware sister builder LogConsentRecordedTx below; this fire-and-forget
// variant is kept for the legacy single-purpose path.
func LogConsentRecorded(ctx context.Context, l Logger, userID uuid.UUID, purpose, policyVersion string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentRecorded,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(ConsentRecordedDetails{Purpose: purpose, PolicyVersion: policyVersion}),
	})
}

// ---- consent builders -----------------------------

// LogConsentRecordedTx records the Register-flow consent INSERT inside
// the caller's pgx.Tx. Mirrors LogUserSelfDeletedTx — the audit row
// commits atomically with the user_consents UPSERTs so a rollback wipes
// both (152-ФЗ forensic invariant ).
//
// purposes packs the three slugs ["tos","privacy","pdn"]; policyVersion
// is the build's current version at write time; policySHA256 may be
// empty (frontend-computed; not plumbed server-side in v1.4).
//
// user_email_at_event is intentionally empty: Register tx doesn't have
// the user resolver wired (the row doesn't exist yet at audit-write
// time); for consent re-record + withdrawal the resolver path is also
// unused because the audit row is tx-scoped not Logger-routed.
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
// as the UPSERTs. fromVersion is the user's prior version (used for
// timeline analysis); toVersion is the build's currentVersion.
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
// inside the user_consents.withdrawn_at UPDATE tx. purpose is "pdn"
// (or "tos"/"privacy" if v1.5 ever differentiates; v1.4 always
// withdraws via /pdn/withdraw and that triggers the deletion bundle).
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
// when DiffAgainstCurrent reports stale policies. No tx (the request is
// read-only). Helps support debug why a user is seeing the modal.
func LogConsentReconsentRequired(ctx context.Context, l Logger, userID uuid.UUID, policies []string, currentVersion string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentReconsentRequired,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(ConsentReconsentRequiredDetails{Policies: policies, CurrentVersion: currentVersion}),
	})
}

// LogConsentPolicyVersionBumped is fire-and-forget system event with no
// UserID. Emitted once per environment when the API detects a new
// build's currentVersion exceeds the most-recent recorded one (Phase
// 22-03 wires this; this plan only declares the builder).
func LogConsentPolicyVersionBumped(ctx context.Context, l Logger, slug, fromVersion, toVersion, sha256 string) {
	l.Log(ctx, Entry{
		Action:   ActionConsentPolicyVersionBumped,
		Resource: "policy",
		UserID:   nil, // system event — no actor.
		Details:  mustMarshal(ConsentPolicyVersionBumpedDetails{Slug: slug, FromVersion: fromVersion, ToVersion: toVersion, SHA256: sha256}),
	})
}

// ---- account.* (deletion) -----------------------------------

// LogDeletionRequested records account.deletion_requested when the user
// soft-deletes their account via DELETE /users/me. orphanedBusinessIDs is
// the list of businesses that would have been orphaned by this deletion
// — currently always empty because the handler returns 409 for any
// sole-owner case, but the field is recorded for forward-compatibility
// with v1.5 ownership-transfer.
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
// more businesses. Telemetry-grade record of attempts the friendly 409
// path rejected (T-DEL-02 mitigation visibility).
func LogSoleOwnerBlocked(ctx context.Context, l Logger, userID uuid.UUID, ip, ua string, soleOwnerBusinessIDs []uuid.UUID) {
	l.Log(ctx, Entry{
		Action:   ActionSoleOwnerBlocked,
		Resource: "user",
		UserID:   &userID,
		Details:  mustMarshal(SoleOwnerBlockedDetails{IP: ip, UserAgent: ua, BusinessIDs: soleOwnerBusinessIDs}),
	})
}

// LogUserSelfDeletedTx is INTENTIONALLY called WITHIN the HardDelete PG
// TX — caller passes the in-flight pgx.Tx so the audit insert is
// atomic with the user row deletion. Signature differs from the other
// builders (which go through the async Logger) because the audit row
// MUST land before the DELETE so the FK SET NULL from 21-03 has
// somewhere to land + user_email_at_event preserves the email for
// 152-ФЗ forensic queries.
func LogUserSelfDeletedTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, originalEmail string) error {
	const q = `INSERT INTO audit_logs (id, user_id, user_email_at_event, action, resource, details, created_at)
	           VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, NOW())`
	if _, err := tx.Exec(ctx, q, userID, originalEmail, ActionUserSelfDeleted, "user", []byte(`{}`)); err != nil {
		return fmt.Errorf("audit: user_self_deleted insert: %w", err)
	}
	return nil
}

// ---- integration builders -----------------------------------------------

// LogIntegrationConnected records a new integration. : details carry
// only platform + external_id (never token material).
func LogIntegrationConnected(ctx context.Context, l Logger, businessID, actorID, integrationID uuid.UUID, platform, externalID string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationConnected,
		Resource:   "integration",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(IntegrationConnectedDetails{IntegrationID: integrationID, Platform: platform, ExternalID: externalID}),
	})
}

// LogIntegrationDisconnected records an integration being removed. Capture
// platform BEFORE the row is deleted.
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
// is intentionally nil (no user actor — system event).
func LogIntegrationTokenRotated(ctx context.Context, l Logger, businessID, integrationID uuid.UUID, platform string) {
	l.Log(ctx, Entry{
		Action:     ActionIntegrationTokenRotated,
		Resource:   "integration",
		BusinessID: &businessID,
		Details:    mustMarshal(IntegrationTokenRotatedDetails{IntegrationID: integrationID, Platform: platform}),
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

// LogBusinessUpdated records a business update. v1 ships without per-field
// diff (Assumption A3).
func LogBusinessUpdated(ctx context.Context, l Logger, businessID, actorID uuid.UUID) {
	l.Log(ctx, Entry{
		Action:     ActionBusinessUpdated,
		Resource:   "business",
		BusinessID: &businessID,
		UserID:     &actorID,
		Details:    mustMarshal(BusinessUpdatedDetails{}),
	})
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
