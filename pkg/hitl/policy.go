// Package hitl contains pure policy-resolution helpers used by both the
// orchestrator (at pause time) and the API (at resolve time, for the TOCTOU
// re-check).
//
// The package is DELIBERATELY dep-free: no persistence, no cache, no message
// bus. It imports only pkg/domain. The identical logic lives in one place
// so pause-time and resolve-time decisions cannot diverge.
package hitl

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// Resolve returns the strictest ToolFloor for toolName given:
//
//	floor            — the registry's minimum (set at registration)
//	businessPolicy   — businesses.settings.tool_approvals map
//	projectOverride  — projects.approval_overrides map
//	toolName         — the full tool name including the {platform}__ prefix
//
// Strictness order: Forbidden > Manual > Auto. No map entry can lower
// strictness below the registry floor; this function never returns a
// strictness below `floor`.
//
// Absence of a key in either map means "inherit" (see Overview §Anti-Footguns
// invariant #8 — inherit is encoded as KEY ABSENCE, never as a literal string
// value). Malformed entries with invalid ToolFloor values are tolerated: since
// ToolFloorRank returns -1 for unknown values, they can never dominate a
// validly-registered floor.
//
// This function is pure — no I/O, no package-level state, no goroutines.
func Resolve(
	floor domain.ToolFloor,
	businessPolicy map[string]domain.ToolFloor,
	projectOverride map[string]domain.ToolFloor,
	toolName string,
) domain.ToolFloor {
	effective := floor
	if biz, ok := businessPolicy[toolName]; ok {
		effective = strictest(effective, biz)
	}
	if proj, ok := projectOverride[toolName]; ok {
		effective = strictest(effective, proj)
	}
	return effective
}

// strictest returns whichever of a, b has the higher ToolFloorRank. Ties and
// unknown values break toward a (the left-hand side, i.e. the running
// accumulator in Resolve), which keeps a valid running value safe against a
// malformed override.
func strictest(a, b domain.ToolFloor) domain.ToolFloor {
	if domain.ToolFloorRank(b) > domain.ToolFloorRank(a) {
		return b
	}
	return a
}

// FloorOf is the registry-floor lookup signature for Bucket. The
// orchestrator's *toolregistry.Registry satisfies this shape directly via
// its Floor method; tests can pass any func(string) domain.ToolFloor. The
// indirection keeps pkg/hitl free of a dependency on the orchestrator's
// internal registry.
type FloorOf func(toolName string) domain.ToolFloor

// Bucket classifies a batch of LLM-proposed tool calls into the three
// dispatch buckets stepRun cares about. Each call goes through the same
// FloorOf → Resolve pipeline as the single-tool path, so a bucketed
// classification can never diverge from a hypothetical per-tool call.
//
//   - auto      — Resolve returned ToolFloorAuto (dispatch immediately).
//   - manual    — Resolve returned ToolFloorManual (persist + pause for HITL).
//   - forbidden — Resolve returned ToolFloorForbidden, OR an unknown floor
//     value defensively (mirrors the pre-extraction default branch in
//     stepRun: unknown tools default to Forbidden via registry, and any
//     unrecognized Resolve outcome bucketed Forbidden so it cannot dispatch).
//
// The returned slices preserve the input order. Nil/empty input returns three
// nil slices — callers branch on `len(...) > 0` and pay zero allocations for
// empty buckets. Pure function — no I/O, no goroutines.
func Bucket(
	floorOf FloorOf,
	businessPolicy map[string]domain.ToolFloor,
	projectOverride map[string]domain.ToolFloor,
	calls []llm.ToolCall,
) (auto, manual, forbidden []llm.ToolCall) {
	for _, tc := range calls {
		floor := floorOf(tc.Function.Name)
		effective := Resolve(floor, businessPolicy, projectOverride, tc.Function.Name)
		switch effective {
		case domain.ToolFloorAuto:
			auto = append(auto, tc)
		case domain.ToolFloorManual:
			manual = append(manual, tc)
		case domain.ToolFloorForbidden:
			forbidden = append(forbidden, tc)
		default:
			forbidden = append(forbidden, tc)
		}
	}
	return
}
