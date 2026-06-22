package chatturn

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestResumeToolArgs pins the args lookup that feeds onToolResult on the resume
// path: the approved (paused) tool resolves from the persisted ToolCalls via
// callIdx, a tool emitted fresh on resume resolves from recCalls, and anything
// unknown degrades to nil so the token_expired flip falls back to platform-wide
// scoping instead of panicking.
func TestResumeToolArgs(t *testing.T) {
	persisted := []domain.ToolCall{
		{ID: "approved-1", Arguments: map[string]interface{}{"channel_id": "chan-A"}},
	}
	fresh := []domain.ToolCall{
		{ID: "resume-2", Arguments: map[string]interface{}{"group_id": "grp-B"}},
	}
	callIdx := map[string]int{"approved-1": 0}

	if got := resumeToolArgs("approved-1", callIdx, persisted, fresh); got["channel_id"] != "chan-A" {
		t.Errorf("approved tool: want channel_id=chan-A, got %v", got)
	}
	if got := resumeToolArgs("resume-2", callIdx, persisted, fresh); got["group_id"] != "grp-B" {
		t.Errorf("resume-emitted tool: want group_id=grp-B, got %v", got)
	}
	if got := resumeToolArgs("unknown", callIdx, persisted, fresh); got != nil {
		t.Errorf("unknown id: want nil, got %v", got)
	}
	if got := resumeToolArgs("", callIdx, persisted, fresh); got != nil {
		t.Errorf("empty id: want nil, got %v", got)
	}
	if got := resumeToolArgs("approved-1", map[string]int{"approved-1": 99}, persisted, fresh); got != nil {
		t.Errorf("out-of-range idx must not panic and must be nil, got %v", got)
	}
}
