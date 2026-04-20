// Package hitlvalidation holds POLICY-07's startup-validation primitives for
// the HITL tool-approval whitelist (Phase 16).
//
// Both services/api (which owns the business + project repositories) and
// services/orchestrator (which owns the live tool registry) need to agree on
// the interpretation of "unknown tool in tool_approvals or approval_overrides".
// Placing the validator in pkg/ keeps the logic in exactly one place — every
// caller interprets a missing tool the same way: treat as denied (POLICY-07
// safe default), log a warning tagged `tool_approval_whitelist_unknown`, do
// NOT auto-prune the setting.
//
// The runtime enforcement is in services/orchestrator/internal/tools.Registry:
// Floor(unknownTool) returns ToolFloorForbidden. This package's
// ValidateApprovalSettings only produces observability (log lines) — it
// never mutates configuration, so a misconfigured map cannot brick startup.
package hitlvalidation

import (
	"context"
	"log/slog"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// ApprovalSource is the generic input shape for business-scoped or
// project-scoped approval overrides. Decoupling the validator from concrete
// Business/Project types lets the API service adapt its repo outputs cheaply
// without dragging repository interfaces into pkg/.
//
// For a Business, build it from `domain.Business.ToolApprovals()`.
// For a Project, build it from `domain.Project.ApprovalOverrides`.
type ApprovalSource struct {
	// ID is the stable identifier used in the log line for operator triage
	// (e.g., the business or project UUID as a string).
	ID string
	// Overrides is the typed map of `tool_name → ToolFloor` being validated.
	// An empty or nil map is valid (produces no warnings).
	Overrides map[string]domain.ToolFloor
}

// ValidateApprovalSettings logs a warning for every tool name referenced by a
// business's `tool_approvals` or a project's `approval_overrides` that is NOT
// present in the live registry. Unknown entries are treated as denied by the
// runtime policy resolver (POLICY-07 safe default — Registry.Floor returns
// ToolFloorForbidden for unknown tools).
//
// This function is pure logging — it does not mutate configuration. Callers
// who want to auto-prune unknowns must do so explicitly after review.
//
// Returns the total warning count so the caller can emit a single summary
// log line at boot (e.g., `slog.Info("tool_approval_whitelist_unknown count", "count", N)`).
//
// The log event key is stable: `tool_approval_whitelist_unknown`. Grafana
// dashboards keyed on this string will break if renamed.
func ValidateApprovalSettings(
	ctx context.Context,
	registeredTools map[string]struct{},
	businesses []ApprovalSource,
	projects []ApprovalSource,
) int {
	warnCount := 0
	for _, b := range businesses {
		for toolName := range b.Overrides {
			if _, ok := registeredTools[toolName]; !ok {
				slog.WarnContext(ctx, "tool_approval_whitelist_unknown",
					"scope", "business",
					"id", b.ID,
					"tool", toolName,
					"action", "treated_as_denied",
				)
				warnCount++
			}
		}
	}
	for _, p := range projects {
		for toolName := range p.Overrides {
			if _, ok := registeredTools[toolName]; !ok {
				slog.WarnContext(ctx, "tool_approval_whitelist_unknown",
					"scope", "project",
					"id", p.ID,
					"tool", toolName,
					"action", "treated_as_denied",
				)
				warnCount++
			}
		}
	}
	return warnCount
}
