// Package service exposes the HITL approval resolve logic and the
// orchestrator-backed tools registry cache.
//
// See docs/services/hitl.md for business rules, lifecycle, and error
// semantics.
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
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// defaultToolsRegistryCacheTTL is the fallback TTL when callers pass a
// non-positive value to NewToolsRegistryCache.
const defaultToolsRegistryCacheTTL = 5 * time.Minute

// ErrHITLBatchNotFound is returned when batch_id is not in the collection (→ 404).
var ErrHITLBatchNotFound = errors.New("hitl: batch not found")

// ErrHITLForbidden is returned when the actor's business does not own the batch (→ 403).
var ErrHITLForbidden = errors.New("hitl: cross-tenant access forbidden")

// ErrHITLBatchExpired is returned when the batch has passed its TTL window (→ 410).
var ErrHITLBatchExpired = errors.New("hitl: approval expired")

// ErrHITLBatchAlreadyResolving is returned when a concurrent resolve won the race (→ 409).
var ErrHITLBatchAlreadyResolving = errors.New("hitl: batch is already resolving")

// ErrHITLDecisionsShape signals that the decisions array does not exactly cover the batch's calls.
// Missing carries the uncovered call_ids; the handler echoes it into the 400 body.
type ErrHITLDecisionsShape struct {
	Missing []string
}

func (e *ErrHITLDecisionsShape) Error() string {
	return fmt.Sprintf("hitl: decisions shape mismatch, missing call_ids: %v", e.Missing)
}

// ErrHITLRejectReasonTooLong signals that a reject_reason exceeds MaxRejectReasonChars.
type ErrHITLRejectReasonTooLong struct {
	Max int
	Got int
}

func (e *ErrHITLRejectReasonTooLong) Error() string {
	return fmt.Sprintf("hitl: reject_reason too long (max %d, got %d)", e.Max, e.Got)
}

// MaxRejectReasonChars caps the user-supplied reject_reason free-form text.
// The frontend textarea enforces the same limit.
const MaxRejectReasonChars = 500

// HITLService wires the pending-tool-call repo, business/project repos, tool
// registry cache, and orchestrator client behind the Resolve entry point.
// See docs/services/hitl.md.
type HITLService struct {
	pendingRepo  domain.PendingToolCallRepository
	businessRepo domain.BusinessRepository
	projectRepo  domain.ProjectRepository
	toolsCache   *ToolsRegistryCache
	orch         *orchestratorclient.Client
}

// NewHITLService constructs a HITLService; a nil dep is a wiring bug and panics.
func NewHITLService(
	pendingRepo domain.PendingToolCallRepository,
	businessRepo domain.BusinessRepository,
	projectRepo domain.ProjectRepository,
	toolsCache *ToolsRegistryCache,
	orch *orchestratorclient.Client,
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
	if orch == nil {
		panic("NewHITLService: orch cannot be nil")
	}
	return &HITLService{
		pendingRepo:  pendingRepo,
		businessRepo: businessRepo,
		projectRepo:  projectRepo,
		toolsCache:   toolsCache,
		orch:         orch,
	}
}

// PendingRepo exposes the pending-tool-call repo for the resume-path handler's pre-flight status check.
func (s *HITLService) PendingRepo() domain.PendingToolCallRepository { return s.pendingRepo }

// BusinessRepo exposes the business repo so the resume handler can re-fetch fresh tool_approvals.
func (s *HITLService) BusinessRepo() domain.BusinessRepository { return s.businessRepo }

// ProjectRepo exposes the project repo for the same fresh-fetch reason.
func (s *HITLService) ProjectRepo() domain.ProjectRepository { return s.projectRepo }

// ToolsCache exposes the shared tools-registry cache used by tools/approvals handlers.
func (s *HITLService) ToolsCache() *ToolsRegistryCache { return s.toolsCache }

// OrchClient exposes the shared orchestrator HTTP client.
func (s *HITLService) OrchClient() *orchestratorclient.Client { return s.orch }

// DecisionInput is the per-call verdict submitted in the resolve body.
// Action is one of "approve" | "edit" | "reject".
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
// Reason is populated only when Action was rewritten server-side (e.g. policy_revoked).
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

// Resolve is the atomic business-logic entry point for the HITL resolve endpoint.
// See docs/services/hitl.md for the ordered lifecycle and TOCTOU rules.
func (s *HITLService) Resolve(ctx context.Context, in ResolveInput) (*ResolveResult, error) {
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

	if missing := missingCallIDs(batch.Calls, in.Decisions); len(missing) > 0 {
		return nil, &ErrHITLDecisionsShape{Missing: missing}
	}

	decisionByID := make(map[string]DecisionInput, len(in.Decisions))
	for _, d := range in.Decisions {
		decisionByID[d.ID] = d
	}

	for _, c := range batch.Calls {
		d := decisionByID[c.CallID]
		if d.Action == "edit" {
			editable := s.toolsCache.EditableFields(c.ToolName)
			if err := tools.ValidateEditArgs(c.ToolName, d.EditedArgs, editable); err != nil {
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

	if _, err := s.pendingRepo.AtomicTransitionToResolving(ctx, in.BatchID); err != nil {
		if errors.Is(err, domain.ErrBatchNotPending) {
			return nil, ErrHITLBatchAlreadyResolving
		}
		if errors.Is(err, domain.ErrBatchNotFound) {
			return nil, ErrHITLBatchNotFound
		}
		return nil, fmt.Errorf("atomic transition to resolving: %w", err)
	}

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
		finalized[i] = c
		finalized[i].Verdict = d.Action
		finalized[i].EditedArgs = d.EditedArgs
		finalized[i].RejectReason = d.RejectReason

		if d.Action == "approve" || d.Action == "edit" {
			floor := c.FloorAtPause
			effective := pkghitl.Resolve(floor, businessApprovals, projectOverrides, c.ToolName)
			if effective == domain.ToolFloorForbidden {
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

	if err := s.pendingRepo.RecordDecisions(ctx, in.BatchID, finalized); err != nil {
		return nil, fmt.Errorf("record decisions: %w", err)
	}

	for _, fc := range finalized {
		metrics.IncHITLDecision(fc.Verdict)
	}

	return result, nil
}

// missingCallIDs returns the call_ids in the batch not covered by decisions.
// See docs/services/hitl.md for the strict-shape / pigeonhole rationale.
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
	if len(missing) == 0 && len(decisions) != len(calls) {
		return nil
	}
	return missing
}

// ToolsRegistryCache is a concurrent-safe in-memory cache over the
// orchestrator's GET /internal/tools projection with per-locale TTL.
// See docs/services/hitl.md for stale-on-error and fail-closed semantics.
type ToolsRegistryCache struct {
	orch *orchestratorclient.Client // nil when orchestratorURL was empty (test/seed-only mode)
	ttl  time.Duration

	mu sync.RWMutex
	// byLocale stores one snapshot per language tag (Tag.String()-keyed —
	// "ru", "en", "" for unspecified). Each snapshot independently tracks
	// its own loadedAt so the TTL works per-locale.
	byLocale map[string]localeSnapshot
	inFlight map[string]chan struct{} // refresh-in-flight guard, per locale
	// latestEntries is the most recently loaded snapshot, used for the
	// locale-agnostic Floor/EditableFields/Has lookups.
	latestEntries []ToolsRegistryEntry
}

type localeSnapshot struct {
	entries  []ToolsRegistryEntry
	loadedAt time.Time
}

// ToolsRegistryEntry is the per-tool projection shared by GET /api/v1/tools
// and GET /internal/tools. Aliased to domain.ToolEntry so the orchestrator
// and the API share one canonical type; field docs live in
// pkg/domain/tool_entry.go.
type ToolsRegistryEntry = domain.ToolEntry

// NewToolsRegistryCache constructs a cache bound to orchestratorURL.
// httpClient=nil uses http.DefaultClient; ttl<=0 defaults to 5 minutes.
func NewToolsRegistryCache(orchestratorURL string, httpClient *http.Client, ttl time.Duration) *ToolsRegistryCache {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if ttl <= 0 {
		ttl = defaultToolsRegistryCacheTTL
	}
	c := &ToolsRegistryCache{
		ttl:      ttl,
		byLocale: make(map[string]localeSnapshot),
		inFlight: make(map[string]chan struct{}),
	}
	if orchestratorURL != "" {
		c.orch = orchestratorclient.New(orchestratorURL, httpClient)
	}
	return c
}

// Seed pre-populates the cache with a static snapshot for all supported locales.
func (c *ToolsRegistryCache) Seed(entries []ToolsRegistryEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copied := append([]ToolsRegistryEntry(nil), entries...)
	c.latestEntries = copied
	now := time.Now()
	c.byLocale[""] = localeSnapshot{entries: copied, loadedAt: now}
	c.byLocale["ru"] = localeSnapshot{entries: copied, loadedAt: now}
	c.byLocale["en"] = localeSnapshot{entries: copied, loadedAt: now}
}

// List returns cached entries for the locale on ctx, refreshing on per-locale TTL expiry.
func (c *ToolsRegistryCache) List(ctx context.Context) []ToolsRegistryEntry {
	tag := i18n.LocaleFromContext(ctx)
	key := tag.String()
	c.mu.RLock()
	snap, ok := c.byLocale[key]
	fresh := ok && !snap.loadedAt.IsZero() && time.Since(snap.loadedAt) < c.ttl
	if fresh {
		defer c.mu.RUnlock()
		out := make([]ToolsRegistryEntry, len(snap.entries))
		copy(out, snap.entries)
		return out
	}
	c.mu.RUnlock()

	c.refresh(ctx, key)

	c.mu.RLock()
	defer c.mu.RUnlock()
	snap = c.byLocale[key]
	out := make([]ToolsRegistryEntry, len(snap.entries))
	copy(out, snap.entries)
	return out
}

// Floor returns the ToolFloor for toolName (Forbidden if absent).
// Linear scan beats a map at the ~20-30 tools in v1.3; floor data is
// locale-independent so latestEntries is authoritative.
func (c *ToolsRegistryCache) Floor(toolName string) domain.ToolFloor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.latestEntries {
		if e.Name == toolName {
			return e.Floor
		}
	}
	return domain.ToolFloorForbidden
}

// EditableFields returns the per-tool edit allowlist or nil for unknown tools.
// nil means every edit on this tool is rejected (fail-closed default).
func (c *ToolsRegistryCache) EditableFields(toolName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.latestEntries {
		if e.Name == toolName {
			out := make([]string, len(e.EditableFields))
			copy(out, e.EditableFields)
			return out
		}
	}
	return nil
}

// Has reports whether toolName is currently cached.
func (c *ToolsRegistryCache) Has(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.latestEntries {
		if e.Name == toolName {
			return true
		}
	}
	return false
}

// refresh fetches a fresh snapshot for localeKey via GET /internal/tools.
// On failure, existing entries are preserved (stale-safe).
func (c *ToolsRegistryCache) refresh(ctx context.Context, localeKey string) {
	c.mu.Lock()
	if ch, ok := c.inFlight[localeKey]; ok {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		return
	}
	done := make(chan struct{})
	c.inFlight[localeKey] = done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inFlight, localeKey)
		close(done)
		c.mu.Unlock()
	}()

	if c.orch == nil {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	entries, err := c.orch.ListTools(reqCtx, localeKey)
	if err != nil {
		return
	}
	fresh := make([]ToolsRegistryEntry, len(entries))
	for i, e := range entries {
		fresh[i] = ToolsRegistryEntry{
			Name:            e.Name,
			DisplayName:     e.DisplayName,
			DisplayNameKey:  e.DisplayNameKey,
			Platform:        e.Platform,
			Floor:           domain.ToolFloor(e.Floor),
			EditableFields:  e.EditableFields,
			Description:     e.Description,
			UserDescription: e.UserDescription,
		}
	}
	c.mu.Lock()
	c.byLocale[localeKey] = localeSnapshot{entries: fresh, loadedAt: time.Now()}
	c.latestEntries = fresh
	c.mu.Unlock()
}
