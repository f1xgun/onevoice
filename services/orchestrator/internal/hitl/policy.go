// Package hitl contains pure policy-resolution helpers used by both the
// orchestrator (at pause time, when deciding whether to request an approval)
// and the API (at resolve time, for the TOCTOU re-check per HITL-06).
//
// The package is DELIBERATELY dep-free: no persistence, no cache, no message
// bus. It is imported by other packages; it imports only pkg/domain. Keep it
// that way so that policy logic lives in exactly one place and is trivially
// table-testable.
package hitl

import "github.com/f1xgun/onevoice/pkg/domain"

// Resolve returns the strictest ToolFloor for toolName given:
//
//	floor            — the registry's minimum (set at registration, POLICY-01)
//	businessPolicy   — businesses.settings.tool_approvals map (POLICY-02)
//	projectOverride  — projects.approval_overrides map (POLICY-03)
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
