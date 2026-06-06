package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// approvalSource is the generic input shape for business-scoped or
// project-scoped approval overrides — decouples validateApprovalSettings from
// concrete Business/Project domain types.
//
// For a Business, build it from `domain.Business.ToolApprovals()`.
// For a Project, build it from `domain.Project.ApprovalOverrides`.
//
// Inlined from pkg/hitlvalidation (deleted): the validator had exactly one
// caller — this file — and 89 LOC of package boilerplate around one log loop.
// Keeping the validator co-located with the sweep concentrates the logic that
// reads the live tool registry, queries Postgres, and emits the
// tool_approval_whitelist_unknown observability events all in one file.
type approvalSource struct {
	// ID is the stable identifier used in the log line for operator triage
	// (e.g., the business or project UUID as a string).
	ID string
	// Overrides is the typed map of `tool_name → ToolFloor` being validated.
	// An empty or nil map is valid (produces no warnings).
	Overrides map[string]domain.ToolFloor
}

// validateApprovalSettings logs a warning for every tool name referenced by a
// business's `tool_approvals` or a project's `approval_overrides` that is NOT
// present in the live registry. Unknown entries are treated as denied by the
// runtime policy resolver (Registry.Floor returns ToolFloorForbidden for
// unknown tools — safe default).
//
// Pure logging — does not mutate configuration. The log event key is stable:
// `tool_approval_whitelist_unknown`. Grafana dashboards keyed on this string
// will break if renamed.
//
// Returns the total warning count so the caller can emit a single summary
// log line at boot.
func validateApprovalSettings(
	ctx context.Context,
	registeredTools map[string]struct{},
	businesses []approvalSource,
	projects []approvalSource,
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

// pendingSweepLoopInterval — delay between the first failed orchestrator
// fetch and the single retry. Keeps the sweep best-effort and bounded.
const pendingSweepLoopInterval = 5 * time.Second

// RunToolApprovalStartupValidation compares every tool-approval entry
// stored in Postgres against the live orchestrator registry (fetched over
// HTTP via pkg/orchestratorclient) and logs tool_approval_whitelist_unknown
// for entries whose tool no longer exists. Unknown entries are NOT
// auto-pruned; they are treated as denied by the runtime policy resolver
// (Registry.Floor returns ToolFloorForbidden for unknown tools).
//
// Non-blocking, best-effort: runs in a goroutine; one retry after 5s; skips
// silently on sustained failure so a slow/dead orchestrator cannot block API
// boot. The sweep is advisory — production alerts should watch for
// `tool_approval_whitelist_unknown` events in Loki/Grafana.
func RunToolApprovalStartupValidation(parent context.Context, pgPool *pgxpool.Pool, orch *orchestratorclient.Client, fetchTimeout time.Duration) {
	sweepCtx, cancel := context.WithTimeout(parent, startupTimeout)
	defer cancel()

	orchURL := ""
	if orch != nil {
		orchURL = orch.BaseURL()
	}
	registered, err := fetchOrchestratorToolNames(sweepCtx, orch, fetchTimeout)
	if err != nil {
		slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: fetch registry failed, retrying",
			"orchestrator", orchURL, "error", err,
		)
		select {
		case <-time.After(pendingSweepLoopInterval):
		case <-sweepCtx.Done():
			return
		}
		registered, err = fetchOrchestratorToolNames(sweepCtx, orch, fetchTimeout)
		if err != nil {
			slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: skipped (orchestrator unreachable)",
				"orchestrator", orchURL, "error", err,
			)
			return
		}
	}

	businesses, err := loadBusinessApprovalSources(sweepCtx, pgPool)
	if err != nil {
		slog.ErrorContext(sweepCtx, "tool_approval_whitelist_sweep: failed to load businesses", "error", err)
		return
	}
	projects, err := loadProjectApprovalSources(sweepCtx, pgPool)
	if err != nil {
		slog.ErrorContext(sweepCtx, "tool_approval_whitelist_sweep: failed to load projects", "error", err)
		return
	}

	count := validateApprovalSettings(sweepCtx, registered, businesses, projects)
	slog.InfoContext(sweepCtx, "tool_approval_whitelist_unknown count",
		"count", count,
		"businesses_scanned", len(businesses),
		"projects_scanned", len(projects),
	)
}

// fetchOrchestratorToolNames calls GET {orchestratorURL}/internal/tools/names
// via pkg/orchestratorclient and returns the resulting set. fetchTimeout
// bounds each individual call; the caller handles retry. nil orch is
// rejected up-front (the API config always supplies a non-empty
// orchestrator URL — a nil here is a wiring bug).
func fetchOrchestratorToolNames(ctx context.Context, orch *orchestratorclient.Client, fetchTimeout time.Duration) (map[string]struct{}, error) {
	if orch == nil {
		return nil, fmt.Errorf("orchestrator client is nil")
	}
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	return orch.ListToolNames(reqCtx)
}

// loadBusinessApprovalSources reads every business's tool_approvals JSONB
// entry directly from Postgres. Materialized into the typed
// approvalSource shape so the validator stays decoupled from
// domain.Business. Skips businesses with no settings payload entirely.
func loadBusinessApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]approvalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(settings, '{}'::jsonb)::text FROM businesses")
	if err != nil {
		return nil, fmt.Errorf("query businesses: %w", err)
	}
	defer rows.Close()

	var out []approvalSource
	for rows.Next() {
		var (
			id       uuid.UUID
			settings string
		)
		if err := rows.Scan(&id, &settings); err != nil {
			return nil, fmt.Errorf("scan business row: %w", err)
		}
		overrides := extractToolApprovals(settings)
		if len(overrides) == 0 {
			continue
		}
		out = append(out, approvalSource{
			ID:        id.String(),
			Overrides: overrides,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate businesses: %w", err)
	}
	return out, nil
}

// loadProjectApprovalSources reads every project's approval_overrides JSONB
// column. Uses COALESCE so older projects (null column) are surfaced as
// empty maps, not as an error.
func loadProjectApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]approvalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(approval_overrides, '{}'::jsonb)::text FROM projects")
	if err != nil {
		slog.WarnContext(ctx, "tool_approval_whitelist_sweep: projects query failed, skipping projects",
			"error", err,
		)
		return nil, nil
	}
	defer rows.Close()

	var out []approvalSource
	for rows.Next() {
		var (
			id        uuid.UUID
			overrides string
		)
		if err := rows.Scan(&id, &overrides); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		parsed := parseToolFloorMap(overrides)
		if len(parsed) == 0 {
			continue
		}
		out = append(out, approvalSource{
			ID:        id.String(),
			Overrides: parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return out, nil
}

// extractToolApprovals pulls the tool_approvals sub-object out of a
// businesses.settings JSONB payload. Returns an empty map if settings is
// malformed, missing, or if tool_approvals is absent. Any non-ToolFloor
// values are dropped silently (the startup sweep treats them as noise; the
// runtime resolver also ignores them via domain.Business.ToolApprovals()).
func extractToolApprovals(settingsJSON string) map[string]domain.ToolFloor {
	var outer map[string]interface{}
	if err := json.Unmarshal([]byte(settingsJSON), &outer); err != nil {
		return nil
	}
	raw, ok := outer["tool_approvals"].(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]domain.ToolFloor, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = domain.ToolFloor(s)
	}
	return out
}

// parseToolFloorMap decodes a JSONB string into a map[string]domain.ToolFloor.
// Invalid payloads yield nil — the sweep logs the issue indirectly because an
// empty overrides map produces zero warnings (the zero warnings is still
// safe behavior; a broken column would need a separate alert).
func parseToolFloorMap(s string) map[string]domain.ToolFloor {
	var raw map[string]string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil
	}
	out := make(map[string]domain.ToolFloor, len(raw))
	for k, v := range raw {
		out[k] = domain.ToolFloor(v)
	}
	return out
}
