package chatturn

import (
	"context"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/approvaltelemetry"
)

// SetApprovalTelemetry attaches the optional best-effort sink before serving requests.
func (t *Turn) SetApprovalTelemetry(sink approvaltelemetry.Sink) {
	t.approvalTelemetry = sink
}

func (t *Turn) recordApprovalResult(ctx context.Context, batch *domain.PendingToolCallBatch, callID string, failed bool, seen map[string]bool) {
	if t.approvalTelemetry == nil || batch == nil || seen[callID] {
		return
	}
	for _, call := range batch.Calls {
		kind := approvaltelemetry.Kind(call.ToolName)
		if call.CallID != callID || kind == "" || (call.Verdict != "approve" && call.Verdict != "edit") {
			continue
		}
		seen[callID] = true
		outcome := "success"
		if failed {
			outcome = sseEventError
		}
		id := resumeApprovalID(batch.ID, callID)
		t.approvalTelemetry.RecordApproval(ctx, approvaltelemetry.Event{
			BatchID: batch.ID, DraftID: id, ApprovalID: id, Kind: kind,
			Source: "chat", Action: "send_result", Outcome: outcome,
		})
		return
	}
}
