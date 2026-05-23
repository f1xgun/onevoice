// Package audit provides typed, async, fire-and-forget writers for the
// audit_logs PostgreSQL table.
//
// Design (per Phase 19 CONTEXT D-05..D-16):
//
//   - Every audit write is async: callers do not block. Logger.Log spawns
//     a goroutine using a detached context.Background() (NOT the request
//     ctx, which cancels on response write).
//
//   - Bounded retry: 3 attempts with exponential backoff 1s/2s/4s plus
//     jitter. On exhaustion, increments audit_log_write_failures_total and
//     logs via slog. No DLQ in v1 (D-07/D-08).
//
//   - Typed builders only: callers invoke LogRoleGranted, LogLoginFailed,
//     etc. The builder constructs a typed Details struct and json.Marshals
//     it. There is no map[string]any escape hatch (D-10).
//
//   - PII rules (D-13/D-14/D-15/D-16):
//   - Auth events store IP + User-Agent + email (never password).
//   - Integration events store platform + external_id (NEVER token,
//     secret, cookie, or session material).
//   - RBAC events store target user_id + role_id (names resolved at
//     read time, not snapshotted).
//   - No request body / form data is logged.
//
// The single public Logger interface lets call sites accept a Logger by
// dependency injection; tests inject a noop or capturing impl.
//
// # auth.token_refreshed deferred
//
// Plan 19-02 ships 21 actions, not 22. The auth.token_refreshed event was
// intentionally excluded per Assumption A2 in 19-RESEARCH.md: every
// access-token expiry would emit one, and the high cardinality would
// dominate the table for low forensic value. Reconsider if audit needs to
// detect token-replay attacks (backlog).
package audit
