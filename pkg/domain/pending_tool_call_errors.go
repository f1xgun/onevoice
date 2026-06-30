package domain

import "errors"

// Pending-tool-call batch errors — owned by the HITL persistence layer.
// These sentinel errors are returned by the
// PendingToolCallRepository implementations in services/api/ and
// services/orchestrator/ so HTTP handlers can map them to 404 / 409.
//
// Rationale:
//   - ErrBatchNotFound  → 404. The _id is missing entirely.
//   - ErrBatchNotPending → 409. The batch exists but its status is not
//     "pending" (concurrent resolve already won, or it was already
//     resolved/expired). Handlers return 409 {retry_after_ms, reason}.
//
// These are exported so both services can reference the exact same
// sentinel — do not duplicate per-service, the handler logic depends on
// errors.Is matching across the repo boundary.
var (
	ErrBatchNotFound   = errors.New("pending batch not found")
	ErrBatchNotPending = errors.New("pending batch is not in status=pending (concurrent resolve or already resolved)")
	// ErrBatchNotResolving is returned by AtomicTransitionResolvingToResuming when
	// the batch exists but is not in status="resolving" — a concurrent /resume
	// already claimed the post-approval continuation (status="resuming"), or the
	// batch has moved on to "resolved"/"expired". Handlers map it to 409.
	ErrBatchNotResolving = errors.New("pending batch is not in status=resolving (concurrent resume or already resolved)")
)
