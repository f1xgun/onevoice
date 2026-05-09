package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitlvalidation"
)

// pendingSweepLoopInterval — delay between the first failed orchestrator
// fetch and the single retry. Keeps the sweep best-effort and bounded.
const pendingSweepLoopInterval = 5 * time.Second

// RunToolApprovalStartupValidation implements POLICY-07 — compares every
// tool-approval entry stored in Postgres against the live orchestrator
// registry (fetched over HTTP) and logs tool_approval_whitelist_unknown for
// entries whose tool no longer exists. Unknown entries are NOT auto-pruned;
// they are treated as denied by the runtime policy resolver (Registry.Floor
// returns ToolFloorForbidden for unknown tools — enforced in Plan 16-03 Task 1).
//
// Non-blocking, best-effort: runs in a goroutine; one retry after 5s; skips
// silently on sustained failure so a slow/dead orchestrator cannot block API
// boot. The sweep is advisory — production alerts should watch for
// `tool_approval_whitelist_unknown` events in Loki/Grafana.
func RunToolApprovalStartupValidation(_ context.Context, pgPool *pgxpool.Pool, orchestratorURL string) {
	sweepCtx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	registered, err := fetchOrchestratorToolNames(sweepCtx, orchestratorURL)
	if err != nil {
		slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: fetch registry failed, retrying",
			"orchestrator", orchestratorURL, "error", err,
		)
		select {
		case <-time.After(pendingSweepLoopInterval):
		case <-sweepCtx.Done():
			return
		}
		registered, err = fetchOrchestratorToolNames(sweepCtx, orchestratorURL)
		if err != nil {
			slog.WarnContext(sweepCtx, "tool_approval_whitelist_sweep: skipped (orchestrator unreachable)",
				"orchestrator", orchestratorURL, "error", err,
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

	count := hitlvalidation.ValidateApprovalSettings(sweepCtx, registered, businesses, projects)
	slog.InfoContext(sweepCtx, "tool_approval_whitelist_unknown count",
		"count", count,
		"businesses_scanned", len(businesses),
		"projects_scanned", len(projects),
	)
}

// fetchOrchestratorToolNames calls GET {orchestratorURL}/internal/tools/names
// and decodes the `{names: [...]}` response into a map usable by
// hitlvalidation.ValidateApprovalSettings. A 10s timeout protects against
// a hung orchestrator; the caller handles retry.
func fetchOrchestratorToolNames(ctx context.Context, orchestratorURL string) (map[string]struct{}, error) {
	reqCtx, cancel := context.WithTimeout(ctx, orchestratorFetchTimeout)
	defer cancel()

	u := strings.TrimRight(orchestratorURL, "/") + "/internal/tools/names"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make(map[string]struct{}, len(body.Names))
	for _, n := range body.Names {
		out[n] = struct{}{}
	}
	return out, nil
}

// loadBusinessApprovalSources reads every business's tool_approvals JSONB
// entry directly from Postgres. Materialized into the typed
// hitlvalidation.ApprovalSource shape so the validator stays decoupled from
// domain.Business. Skips businesses with no settings payload entirely.
func loadBusinessApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]hitlvalidation.ApprovalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(settings, '{}'::jsonb)::text FROM businesses")
	if err != nil {
		return nil, fmt.Errorf("query businesses: %w", err)
	}
	defer rows.Close()

	var out []hitlvalidation.ApprovalSource
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
		out = append(out, hitlvalidation.ApprovalSource{
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
// column. Uses COALESCE so projects predating Phase 16 (null column) are
// surfaced as empty maps, not as an error.
func loadProjectApprovalSources(ctx context.Context, pool *pgxpool.Pool) ([]hitlvalidation.ApprovalSource, error) {
	rows, err := pool.Query(ctx, "SELECT id, COALESCE(approval_overrides, '{}'::jsonb)::text FROM projects")
	if err != nil {
		// Graceful degradation: if approval_overrides column doesn't yet exist
		// (migration ordering race in dev), skip projects rather than failing
		// the entire sweep.
		slog.WarnContext(ctx, "tool_approval_whitelist_sweep: projects query failed, skipping projects",
			"error", err,
		)
		return nil, nil
	}
	defer rows.Close()

	var out []hitlvalidation.ApprovalSource
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
		out = append(out, hitlvalidation.ApprovalSource{
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
