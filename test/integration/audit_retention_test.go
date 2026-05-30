package integration

// Audit-log retention across hard-delete.
//
// End-to-end gate for the FK CASCADE → SET NULL migration (000014/000012)
// + the loggerImpl.write user_email_at_event population path:
//
// 1. Register a user.
// 2. Trigger any audit-emitting action (Register itself emits
// auth.user_registered + auth.consent_recorded asynchronously).
// 3. Poll for the audit row + assert user_email_at_event matches the
// registration email (resolver populated it BEFORE the INSERT).
// 4. Hard-delete the user (DELETE FROM users WHERE id = $1; 
// will wrap this in a proper service path).
// 5. Re-query the same audit row: user_id IS NULL (FK SET NULL) AND
// user_email_at_event still equals the original email.
//
// This single test is the acceptance gate: the FK migration is
// useless without the resolver, and the resolver is useless without the
// FK migration. Asserting them together as one E2E is the contract.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAuditLog_SurvivesUserDelete(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	cleanupVerify(t)

	email := "audit-retention-" + uuid.NewString()[:8] + "@example.com"
	_ = setupTestUser(t, email, "password123")

	var userID uuid.UUID
	require.NoError(t, pgPool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, email).Scan(&userID))

	// Poll the async audit goroutine — Register emits user_registered.
	var auditID uuid.UUID
	var emailAtEvent string
	require.Eventually(t, func() bool {
		err := pgPool.QueryRow(context.Background(),
			`SELECT id, COALESCE(user_email_at_event, '') FROM audit_logs
			  WHERE user_id = $1 AND action = 'auth.user_registered'
			  ORDER BY created_at DESC LIMIT 1`,
			userID).Scan(&auditID, &emailAtEvent)
		return err == nil
	}, 3*time.Second, 50*time.Millisecond, "audit row for user_registered must land")

	require.Equal(t, email, emailAtEvent,
		"user_email_at_event MUST be populated by the audit logger's UserResolver before INSERT")

	// Hard-delete the user. wraps this in a service path; the
	// SQL is the same primitive.
	_, err := pgPool.Exec(context.Background(),
		`DELETE FROM users WHERE id = $1`, userID)
	require.NoError(t, err, "hard-delete must succeed (FK SET NULL on audit_logs.user_id)")

	// Re-query the SAME audit row by id — user_id is now NULL (CASCADE → SET NULL),
	// but the email survives.
	var nullableUserID *uuid.UUID
	var preservedEmail string
	err = pgPool.QueryRow(context.Background(),
		`SELECT user_id, COALESCE(user_email_at_event, '') FROM audit_logs WHERE id = $1`,
		auditID).Scan(&nullableUserID, &preservedEmail)
	require.NoError(t, err, "audit row MUST still exist after user delete (FK SET NULL, not CASCADE)")
	require.Nil(t, nullableUserID, "user_id MUST be NULL after hard-delete (SET NULL semantics)")
	require.Equal(t, email, preservedEmail,
		"user_email_at_event MUST survive user delete (152-ФЗ Art. 19 audit retention)")
}
