// Package hitl contains pure policy-resolution helpers used by both the
// orchestrator (at pause time, when deciding whether to request an approval)
// and the API (at resolve time, for the TOCTOU re-check per HITL-06).
//
// Plan 16-07 relocation: the canonical pure-function Resolve now lives in
// pkg/hitl so that services/api can import it directly (Go's internal-
// visibility rule blocks cross-module imports of services/orchestrator/
// internal/*). This file is kept as a thin compat shim so existing
// orchestrator callers (stepRun + Resume) do not churn their imports.
//
// The package is DELIBERATELY dep-free: no persistence, no cache, no message
// bus. Keep it that way so that policy logic lives in exactly one place and
// is trivially table-testable.
package hitl

import (
	"github.com/f1xgun/onevoice/pkg/domain"
	pkghitl "github.com/f1xgun/onevoice/pkg/hitl"
)

// Resolve delegates to pkg/hitl.Resolve — see that package for the full
// contract documentation. Single-source-of-truth: orchestrator pause-time and
// API resolve-time use identical logic.
func Resolve(
	floor domain.ToolFloor,
	businessPolicy map[string]domain.ToolFloor,
	projectOverride map[string]domain.ToolFloor,
	toolName string,
) domain.ToolFloor {
	return pkghitl.Resolve(floor, businessPolicy, projectOverride, toolName)
}
