package domain

import "errors"

// Pending-tool-call batch errors — owned by the HITL persistence layer
// (Plan 16-02). These sentinel errors are returned by the
// PendingToolCallRepository implementations in services/api/ and
// services/orchestrator/ so HTTP handlers can map them to 404 / 409.
//
// Rationale (see 16-02 Pattern 2, Research §Atomic Resolve Contract):
//   - ErrBatchNotFound  → 404. The _id is missing entirely.
//   - ErrBatchNotPending → 409. The batch exists but its status is not
//     "pending" (concurrent resolve already won, or it was already
//     resolved/expired). Handlers return 409 {retry_after_ms, reason} per
//     D-03.
//
// These are exported so both services can reference the exact same
// sentinel — do not duplicate per-service, the handler logic depends on
// errors.Is matching across the repo boundary.
var (
	ErrBatchNotFound   = errors.New("pending batch not found")
	ErrBatchNotPending = errors.New("pending batch is not in status=pending (concurrent resolve or already resolved)")
)
