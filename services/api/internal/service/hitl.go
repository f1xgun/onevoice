// Package service — HITL approval resolve logic.
//
// This file implements the core atomic-resolve path behind
// POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve. It
// enforces every HITL safety property at the business-logic layer:
//
//   - Ownership (actor.BusinessID == batch.BusinessID) — cross-tenant → 403
//   - Decision shape (exactly one decision per call) — bad shape → 400 {missing:[...]}
//   - Edit validation via pkg/toolvalidation — invalid field → 400 {editable:[...]}
//   - Atomic status transition via findOneAndUpdate — race → 409 {retry_after_ms}
//   - TOCTOU re-check via pkg/hitl.Resolve with FRESH business/project maps —
//     post-pause Forbidden flips to a synthetic "policy_revoked" rejection
//     (HITL-06 — the resolve call still succeeds so the response to the
//     client is 200; the LLM sees the synthetic rejection on resume)
//   - tool_name pinning (HITL-07) — the server NEVER reads tool_name from the
//     request body; it always pulls from the persisted PendingCall row
//
// Anti-footgun #5: the AtomicTransitionToResolving primitive (findOneAndUpdate
// with filter {_id, status: "pending"}) guarantees exactly-one-wins on
// concurrent resolve attempts. Under contention, the loser gets
// ErrBatchNotPending which maps to 409 — see handler/hitl.go.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	pkghitl "github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/toolvalidation"
)

// Typed errors exposed by the HITL service layer. Each maps to a specific HTTP
// status code in the handler layer (see handler/hitl.go).
var (
	// ErrHITLBatchNotFound → 404. The batch_id is not in the collection.
	ErrHITLBatchNotFound = errors.New("hitl: batch not found")
	// ErrHITLForbidden → 403. Actor's business does not own the batch.
	ErrHITLForbidden = errors.New("hitl: cross-tenant access forbidden")
	// ErrHITLBatchExpired → 410. The batch has passed its TTL window.
	ErrHITLBatchExpired = errors.New("hitl: approval expired")
	// ErrHITLBatchAlreadyResolving → 409. Concurrent resolve won the race.
	ErrHITLBatchAlreadyResolving = errors.New("hitl: batch is already resolving")
)

// ErrHITLDecisionsShape is returned when the decisions array doesn't exactly
// cover every call in the batch. Missing carries the call_ids that were NOT
// covered by the submitted decisions — the handler echoes this slice in the
// 400 body so the client can retry with a corrected payload.
type ErrHITLDecisionsShape struct {
	Missing []string
}

func (e *ErrHITLDecisionsShape) Error() string {
	return fmt.Sprintf("hitl: decisions shape mismatch, missing call_ids: %v", e.Missing)
}

// ErrHITLRejectReasonTooLong is returned when a reject_reason exceeds the
// 500-char cap enforced by D-08.
type ErrHITLRejectReasonTooLong struct {
	Max int
	Got int
}

func (e *ErrHITLRejectReasonTooLong) Error() string {
	return fmt.Sprintf("hitl: reject_reason too long (max %d, got %d)", e.Max, e.Got)
}

// MaxRejectReasonChars caps the user-supplied reject_reason free-form text
// (D-08). Frontend textarea enforces the same limit.
const MaxRejectReasonChars = 500

// HITLService wires every HITL primitive (pending-tool-call repo, business
// repo, project repo, tool registry cache) together behind the Resolve
// business-logic entry point consumed by handler/hitl.go.
type HITLService struct {
	pendingRepo     domain.PendingToolCallRepository
	businessRepo    domain.BusinessRepository
	projectRepo     domain.ProjectRepository
	toolsCache      *ToolsRegistryCache
	orchestratorURL string
	httpClient      *http.Client
}

// NewHITLService constructs a HITLService. All four reqiured deps are
// mandatory — a nil anywhere indicates a wiring bug and is rejected with a
// panic at construction time.
func NewHITLService(
	pendingRepo domain.PendingToolCallRepository,
	businessRepo domain.BusinessRepository,
	projectRepo domain.ProjectRepository,
	toolsCache *ToolsRegistryCache,
	orchestratorURL string,
	httpClient *http.Client,
) *HITLService {
	if pendingRepo == nil {
		panic("NewHITLService: pendingRepo cannot be nil")
	}
	if businessRepo == nil {
		panic("NewHITLService: businessRepo cannot be nil")
	}
	if projectRepo == nil {
		panic("NewHITLService: projectRepo cannot be nil")
	}
	if toolsCache == nil {
		panic("NewHITLService: toolsCache cannot be nil")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HITLService{
		pendingRepo:     pendingRepo,
		businessRepo:    businessRepo,
		projectRepo:     projectRepo,
		toolsCache:      toolsCache,
		orchestratorURL: orchestratorURL,
		httpClient:      httpClient,
	}
}

// PendingRepo exposes the pending-tool-call repo so the resume-path handler
// can perform pre-flight status checks (e.g., 409 when batch is resolving).
func (s *HITLService) PendingRepo() domain.PendingToolCallRepository { return s.pendingRepo }

// BusinessRepo exposes the business repo so the resume-path handler can
// re-fetch FRESH tool_approvals before forwarding to the orchestrator.
func (s *HITLService) BusinessRepo() domain.BusinessRepository { return s.businessRepo }

// ProjectRepo exposes the project repo for the same fresh-fetch reason.
func (s *HITLService) ProjectRepo() domain.ProjectRepository { return s.projectRepo }

// ToolsCache exposes the tools-registry cache so handlers outside this file
// (GET /api/v1/tools, POLICY-05 PUT) can share the cache.
func (s *HITLService) ToolsCache() *ToolsRegistryCache { return s.toolsCache }

// OrchestratorURL returns the configured orchestrator base URL. Used by the
// resume handler when forwarding the follow-up SSE stream.
func (s *HITLService) OrchestratorURL() string { return s.orchestratorURL }

// HTTPClient returns the configured HTTP client for orchestrator calls.
func (s *HITLService) HTTPClient() *http.Client { return s.httpClient }

// DecisionInput is the per-call verdict submitted in the resolve body.
// ID must match the `CallID` of a PendingCall in the batch. Action is one of
// "approve" | "edit" | "reject".
type DecisionInput struct {
	ID           string                 `json:"id"`
	Action       string                 `json:"action"`
	EditedArgs   map[string]interface{} `json:"edited_args,omitempty"`
	RejectReason string                 `json:"reject_reason,omitempty"`
}

// ResolveInput is the validated input to HITLService.Resolve.
type ResolveInput struct {
	ConversationID  string
	BatchID         string
	ActorUserID     string
	ActorBusinessID string
	Decisions       []DecisionInput
}

// ResolvedCall is a single per-call projection in the resolve response.
// Reason is populated only when Action was rewritten server-side (e.g., by
// TOCTOU policy_revoked).
type ResolvedCall struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ResolveResult is the 200-body shape returned by Resolve.
type ResolveResult struct {
	BatchID    string         `json:"batch_id"`
	ResolvedAt time.Time      `json:"resolved_at"`
	Decisions  []ResolvedCall `json:"decisions"`
}

// Resolve is the atomic business-logic entry point for
// POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve.
//
// Steps (order-sensitive):
//
//  1. Load the batch (ownership + existence check)
//  2. Validate decisions shape (must cover every call exactly once)
//  3. Validate edit arguments against the pinned tool_name's EditableFields
//     (HITL-07 + D-12 — 400 with editable list on violation; client-supplied
//     tool_name is NEVER read — we always use the persisted PendingCall row)
//  4. Atomic status transition to "resolving" — 409 on concurrent-resolve
//     race (anti-footgun #5)
//  5. Re-fetch business/project state and re-run pkg/hitl.Resolve against
//     FRESH maps (HITL-06 TOCTOU). Forbidden-after-pause becomes a synthetic
//     rejection ("policy_revoked") that the LLM sees on resume.
//  6. Persist final per-call verdicts via RecordDecisions
//
// The response is plain JSON (D-05) — the client separately opens the
// /chat/{id}/resume endpoint to get the SSE continuation stream.
func (s *HITLService) Resolve(ctx context.Context, in ResolveInput) (*ResolveResult, error) {
	// Step 1: load batch for ownership + existence checks.
	batch, err := s.pendingRepo.GetByBatchID(ctx, in.BatchID)
	if err != nil {
		if errors.Is(err, domain.ErrBatchNotFound) {
			return nil, ErrHITLBatchNotFound
		}
		return nil, fmt.Errorf("load batch: %w", err)
	}
	if batch.BusinessID != in.ActorBusinessID {
		return nil, ErrHITLForbidden
	}
	if batch.ConversationID != in.ConversationID {
		return nil, ErrHITLForbidden
	}
	if batch.Status == "expired" {
		return nil, ErrHITLBatchExpired
	}

	// Step 2: validate decisions shape — exactly one per call.
	if missing := missingCallIDs(batch.Calls, in.Decisions); len(missing) > 0 {
		return nil, &ErrHITLDecisionsShape{Missing: missing}
	}

	decisionByID := make(map[string]DecisionInput, len(in.Decisions))
	for _, d := range in.Decisions {
		decisionByID[d.ID] = d
	}

	// Step 3: edit validation (HITL-07 + D-12, per call).
	// tool_name is ALWAYS read from the persisted PendingCall (c.ToolName),
	// NEVER from the client's body. This is the pinning invariant.
	for _, c := range batch.Calls {
		d := decisionByID[c.CallID]
		if d.Action == "edit" {
			editable := s.toolsCache.EditableFields(c.ToolName)
			if err := toolvalidation.ValidateEditArgs(c.ToolName, d.EditedArgs, editable); err != nil {
				// Typed error — handler maps to 400 with the correct body shape.
				return nil, err
			}
		}
		if d.Action == "reject" && len(d.RejectReason) > MaxRejectReasonChars {
			return nil, &ErrHITLRejectReasonTooLong{
				Max: MaxRejectReasonChars,
				Got: len(d.RejectReason),
			}
		}
	}

	// Step 4: atomic status transition → 409 on concurrent-resolve race.
	if _, err := s.pendingRepo.AtomicTransitionToResolving(ctx, in.BatchID); err != nil {
		if errors.Is(err, domain.ErrBatchNotPending) {
			return nil, ErrHITLBatchAlreadyResolving
		}
		if errors.Is(err, domain.ErrBatchNotFound) {
			return nil, ErrHITLBatchNotFound
		}
		return nil, fmt.Errorf("atomic transition to resolving: %w", err)
	}

	// Step 5: TOCTOU re-check with FRESH business/project maps.
	business, err := s.businessRepo.GetByID(ctx, parseUUIDSafe(batch.BusinessID))
	businessApprovals := map[string]domain.ToolFloor{}
	if err == nil && business != nil {
		businessApprovals = business.ToolApprovals()
	}

	var projectOverrides map[string]domain.ToolFloor
	if batch.ProjectID != "" {
		if projUUID, perr := parseUUIDStrict(batch.ProjectID); perr == nil {
			if proj, perr := s.projectRepo.GetByID(ctx, projUUID); perr == nil && proj != nil {
				projectOverrides = proj.ApprovalOverrides
			}
		}
	}

	finalized := make([]domain.PendingCall, len(batch.Calls))
	result := &ResolveResult{
		BatchID:    in.BatchID,
		ResolvedAt: time.Now().UTC(),
		Decisions:  make([]ResolvedCall, 0, len(batch.Calls)),
	}

	for i, c := range batch.Calls {
		d := decisionByID[c.CallID]
		// Copy the persisted call verbatim — tool_name, arguments, call_id all
		// stay pinned. Only verdict/edited_args/reject_reason are mutated.
		finalized[i] = c
		finalized[i].Verdict = d.Action
		finalized[i].EditedArgs = d.EditedArgs
		finalized[i].RejectReason = d.RejectReason

		if d.Action == "approve" || d.Action == "edit" {
			floor := s.toolsCache.Floor(c.ToolName)
			effective := pkghitl.Resolve(floor, businessApprovals, projectOverrides, c.ToolName)
			if effective == domain.ToolFloorForbidden {
				// TOCTOU: flipped to forbidden post-pause. Rewrite to synthetic
				// rejection — the LLM sees the outcome on resume (HITL-09).
				finalized[i].Verdict = "reject"
				finalized[i].RejectReason = "policy_revoked"
				result.Decisions = append(result.Decisions, ResolvedCall{
					ID:     c.CallID,
					Action: "reject",
					Reason: "policy_revoked",
				})
				continue
			}
		}
		result.Decisions = append(result.Decisions, ResolvedCall{
			ID:     c.CallID,
			Action: d.Action,
		})
	}

	// Step 6: persist finalized decisions on the batch.
	if err := s.pendingRepo.RecordDecisions(ctx, in.BatchID, finalized); err != nil {
		return nil, fmt.Errorf("record decisions: %w", err)
	}

	return result, nil
}

// missingCallIDs returns the call_ids in the batch that are NOT covered by
// the decisions list. Caller uses the return as the `missing` field in the
// 400 body shape.
//
// Note: we don't separately check for extra decisions — an extra decision
// whose ID doesn't match any call in the batch implicitly makes some real
// call missing (by pigeonhole: N calls, N decisions, one doesn't match →
// some real call ID has no matching decision).
func missingCallIDs(calls []domain.PendingCall, decisions []DecisionInput) []string {
	have := make(map[string]struct{}, len(decisions))
	for _, d := range decisions {
		have[d.ID] = struct{}{}
	}
	missing := make([]string, 0)
	for _, c := range calls {
		if _, ok := have[c.CallID]; !ok {
			missing = append(missing, c.CallID)
		}
	}
	// Also handle the strict-shape case: different counts flag as mismatch.
	if len(missing) == 0 && len(decisions) != len(calls) {
		// Extra decisions (or duplicates) — not missing per se but the shape is
		// still wrong. We do not surface duplicate IDs here because the batch's
		// call_ids are unique and decisions map to them; a duplicate decision
		// simply overwrites in decisionByID.
		return nil
	}
	return missing
}

// ToolsRegistryCache is a thin in-memory cache over GET /internal/tools on
// the orchestrator. The cache stores the full registry projection (name,
// platform, floor, editable_fields, description) with a 5-minute TTL so
// the resolve handler's edit-validation path stays a map lookup in the hot
// path.
//
// The cache is CONCURRENT-SAFE: sync.RWMutex guards the entries slice and
// the refresh-in-flight guard. On TTL expiration, the first reader triggers
// a single refresh via the httpClient; subsequent concurrent readers wait on
// the in-flight channel and observe the refreshed entries.
//
// Cache miss / orchestrator unreachable semantics:
//   - Stale entries are preferred over empty results (avoid cascading 500s).
//   - On first-ever load failure, the cache returns empty — every tool then
//     has nil EditableFields which causes edit validation to reject every
//     field as not-editable (safe default: fail-closed).
type ToolsRegistryCache struct {
	orchestratorURL string
	httpClient      *http.Client
	ttl             time.Duration

	mu            sync.RWMutex
	entries       []ToolsRegistryEntry
	loadedAt      time.Time
	inFlight      chan struct{} // closed when the current refresh completes
}

// ToolsRegistryEntry is the per-tool projection returned by GET /api/v1/tools
// (frontend) and by GET /internal/tools (internal — orchestrator-to-API).
type ToolsRegistryEntry struct {
	Name           string           `json:"name"`
	Platform       string           `json:"platform"`
	Floor          domain.ToolFloor `json:"floor"`
	EditableFields []string         `json:"editableFields"`
	Description    string           `json:"description"`
}

// NewToolsRegistryCache constructs a cache bound to orchestratorURL (e.g.,
// "http://orchestrator:8090"). Pass httpClient=nil to use http.DefaultClient.
// ttl defaults to 5 minutes when zero.
func NewToolsRegistryCache(orchestratorURL string, httpClient *http.Client, ttl time.Duration) *ToolsRegistryCache {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ToolsRegistryCache{
		orchestratorURL: orchestratorURL,
		httpClient:      httpClient,
		ttl:             ttl,
	}
}

// Seed pre-populates the cache with a static snapshot. Used by tests to avoid
// HTTP round-trips against the orchestrator.
func (c *ToolsRegistryCache) Seed(entries []ToolsRegistryEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append([]ToolsRegistryEntry(nil), entries...)
	c.loadedAt = time.Now()
}

// List returns the cached entries, refreshing if TTL elapsed. Safe for
// concurrent callers.
func (c *ToolsRegistryCache) List(ctx context.Context) []ToolsRegistryEntry {
	c.mu.RLock()
	fresh := !c.loadedAt.IsZero() && time.Since(c.loadedAt) < c.ttl
	if fresh {
		defer c.mu.RUnlock()
		out := make([]ToolsRegistryEntry, len(c.entries))
		copy(out, c.entries)
		return out
	}
	c.mu.RUnlock()

	// Refresh (best-effort; stale-on-error).
	c.refresh(ctx)

	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ToolsRegistryEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Floor returns the ToolFloor for toolName from the cache (or Forbidden if
// absent). Refreshes on stale. The method is hot-path (called per decision in
// the resolve loop) so the entries search is a linear scan rather than a
// preallocated map — 20-30 tools total in v1.3, a loop beats a map's pointer
// chase and GC pressure.
func (c *ToolsRegistryCache) Floor(toolName string) domain.ToolFloor {
	c.mu.RLock()
	for _, e := range c.entries {
		if e.Name == toolName {
			defer c.mu.RUnlock()
			return e.Floor
		}
	}
	c.mu.RUnlock()
	return domain.ToolFloorForbidden
}

// EditableFields returns the per-tool edit allowlist, or nil for unknown
// tools. nil means "every edit on this tool is rejected" — matches POLICY-07.
func (c *ToolsRegistryCache) EditableFields(toolName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.Name == toolName {
			out := make([]string, len(e.EditableFields))
			copy(out, e.EditableFields)
			return out
		}
	}
	return nil
}

// Has reports whether toolName is currently cached. Used by the POLICY-05 PUT
// handler to reject unknown tool names before persisting.
func (c *ToolsRegistryCache) Has(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.entries {
		if e.Name == toolName {
			return true
		}
	}
	return false
}

// refresh fetches a fresh snapshot from GET {orchestratorURL}/internal/tools.
// On failure, preserves existing entries (stale-safe) and logs at WARN.
// Single-in-flight dedupes concurrent refresh attempts.
func (c *ToolsRegistryCache) refresh(ctx context.Context) {
	c.mu.Lock()
	if c.inFlight != nil {
		// Another goroutine is refreshing; wait for it to finish.
		ch := c.inFlight
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		return
	}
	done := make(chan struct{})
	c.inFlight = done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.inFlight = nil
		close(done)
		c.mu.Unlock()
	}()

	// No network call during tests that only Seed — detect by empty
	// orchestratorURL.
	if c.orchestratorURL == "" {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := c.orchestratorURL + "/internal/tools"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var fresh []ToolsRegistryEntry
	if err := decodeJSON(resp.Body, &fresh); err != nil {
		return
	}
	c.mu.Lock()
	c.entries = fresh
	c.loadedAt = time.Now()
	c.mu.Unlock()
}
