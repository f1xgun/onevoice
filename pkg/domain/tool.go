package domain

// ToolFloor is the minimum approval level assigned to a tool at registration.
// It is the baseline applied by the orchestrator BEFORE per-business and
// per-project overrides are layered on top (see POLICY-01..04 and the
// strictest-wins resolver in services/orchestrator/internal/hitl.Resolve).
//
// Strictness order (low → high): ToolFloorAuto < ToolFloorManual < ToolFloorForbidden.
// Overrides may only RAISE strictness; no map entry can lower strictness below
// the registry floor.
type ToolFloor string

const (
	// ToolFloorAuto — the tool runs without human approval.
	ToolFloorAuto ToolFloor = "auto"
	// ToolFloorManual — the tool pauses mid-turn for human approve/edit/reject.
	ToolFloorManual ToolFloor = "manual"
	// ToolFloorForbidden — the tool is not permitted regardless of settings.
	ToolFloorForbidden ToolFloor = "forbidden"
)

// ValidToolFloor returns true iff f is one of the three defined ToolFloor
// constants. Empty string, unknown enum values, and mixed-case variants all
// return false (the enum is lowercase by convention, matching WhitelistMode).
func ValidToolFloor(f ToolFloor) bool {
	switch f {
	case ToolFloorAuto, ToolFloorManual, ToolFloorForbidden:
		return true
	}
	return false
}

// ToolFloorRank maps a ToolFloor to an ordered integer so callers can compute
// strictest-wins comparisons without a cascade of switch statements:
//
//	Auto      → 0
//	Manual    → 1
//	Forbidden → 2
//	(invalid) → -1
//
// Invalid values return -1 so they rank BELOW any valid floor — this guarantees
// that a malformed settings entry can never dominate a registered floor.
func ToolFloorRank(f ToolFloor) int {
	switch f {
	case ToolFloorAuto:
		return 0
	case ToolFloorManual:
		return 1
	case ToolFloorForbidden:
		return 2
	}
	return -1
}
