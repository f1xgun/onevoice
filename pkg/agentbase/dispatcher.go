package agentbase

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// Dispatcher executes the per-tool work AND owns the HITL dedupe gate. The
// per-agent tool-routing switch stays platform-specific (different tool names,
// different argument shapes) and is supplied as the exec callback.
//
// Usage in an agent Handler.Handle:
//
//	func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
//	    return h.dispatcher.Dispatch(ctx, req, h.routeTool)
//	}
//
// Where h.routeTool is the platform switch that lived inline in Handle before
// plan 19-07. See phase 19-RESEARCH §5b for the duplication audit that
// motivates this extraction.
type Dispatcher interface {
	Dispatch(
		ctx context.Context,
		req a2a.ToolRequest,
		exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error),
	) (*a2a.ToolResponse, error)
}

// dispatcherImpl wires a *hitldedupe.DedupeClient and an ErrorClassifier into
// the Dispatch sequence. Both deps are optional:
//   - dedupe == nil disables the HITL gate entirely (legacy/auto path).
//   - classifier == nil applies no error-wrapping (errors propagate as-is).
//
// This matches the semantics of the per-agent Handler before extraction: the
// telegram/vk/yandex/google handlers all accept a nil *hitldedupe.DedupeClient
// for unit tests / dev environments without Redis.
type dispatcherImpl struct {
	dedupe     *hitldedupe.DedupeClient
	classifier ErrorClassifier
}

// NewDispatcher wires the dedupe + classifier into the standard sequence:
//
//  1. dedupeGate — return early if in-flight or cached duplicate.
//  2. exec — agent's per-tool work (the switch on req.Tool).
//  3. classifier.Classify — wrap permanent errors as a2a.NonRetryableError.
//  4. dedupeStore — cache successful responses (best-effort; logs on failure).
//
// Both arguments may be nil. NewDispatcher always returns a usable Dispatcher.
func NewDispatcher(dedupe *hitldedupe.DedupeClient, classifier ErrorClassifier) Dispatcher {
	return &dispatcherImpl{dedupe: dedupe, classifier: classifier}
}

// Dispatch implements the Dispatcher interface. The four-step sequence is the
// canonical HITL-aware tool dispatch, lifted out of the four agent
// Handler.Handle methods so it stays consistent across platforms.
func (d *dispatcherImpl) Dispatch(
	ctx context.Context,
	req a2a.ToolRequest,
	exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error),
) (*a2a.ToolResponse, error) {
	if resp, stop := d.dedupeGate(ctx, req); stop {
		return resp, nil
	}

	resp, err := exec(ctx, req)
	if d.classifier != nil {
		err = d.classifier.Classify(err)
	}

	d.dedupeStore(ctx, req, resp, err)
	return resp, err
}

// dedupeGate consults the Redis dedupe cache BEFORE tool dispatch when a HITL
// approval is in play. It returns (resp, true) when the caller should stop
// executing (in-flight elsewhere, or already-completed duplicate). On any
// error the gate is best-effort — we log and fall through rather than fail
// a turn because Redis blinked.
//
// Body is lifted verbatim from
// services/agent-telegram/internal/agent/handler.go:88-117 — the four agent
// implementations were byte-identical apart from comment wording.
func (d *dispatcherImpl) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
	if d.dedupe == nil || req.ApprovalID == "" {
		return nil, false
	}
	outcome, cached, err := d.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
	if err != nil {
		slog.WarnContext(ctx, "hitl dedupe claim failed; proceeding without dedupe",
			"error", err, "business_id", req.BusinessID, "approval_id", req.ApprovalID)
		return nil, false
	}
	switch outcome {
	case hitldedupe.ClaimOutcomeInFlight:
		return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: already in flight"}, true
	case hitldedupe.ClaimOutcomeDuplicate:
		var cachedResp a2a.ToolResponse
		if uerr := json.Unmarshal([]byte(cached), &cachedResp); uerr != nil {
			slog.WarnContext(ctx, "hitl dedupe cached result malformed; returning generic duplicate",
				"error", uerr)
			return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: cached result unavailable"}, true
		}
		// The cached response was stored for the original TaskID; rewrite to
		// this replay's TaskID so the orchestrator correlates correctly.
		cachedResp.TaskID = req.TaskID
		return &cachedResp, true
	case hitldedupe.ClaimOutcomeClaimed, hitldedupe.ClaimOutcomeSkip:
		// Proceed with execution — no cached response.
	}
	return nil, false
}

// dedupeStore persists a successful ToolResponse under the HITL dedupe key so
// replays see ClaimOutcomeDuplicate. Errors and nil responses are NOT cached
// (a replay should be free to retry when the original failed).
//
// Body is lifted verbatim from
// services/agent-telegram/internal/agent/handler.go:118-130. Note that
// hitldedupe.DedupeClient.Store accepts an interface{} and json-marshals it
// internally — we pass *resp directly, not a pre-encoded string.
func (d *dispatcherImpl) dedupeStore(ctx context.Context, req a2a.ToolRequest, resp *a2a.ToolResponse, execErr error) {
	if d.dedupe == nil || req.ApprovalID == "" || execErr != nil || resp == nil {
		return
	}
	if serr := d.dedupe.Store(ctx, req.BusinessID, req.ApprovalID, resp); serr != nil {
		slog.WarnContext(ctx, "hitl dedupe store failed; result returned but not cached",
			"error", serr, "approval_id", req.ApprovalID)
	}
}

// Compile-time check: dispatcherImpl satisfies Dispatcher.
var _ Dispatcher = (*dispatcherImpl)(nil)
