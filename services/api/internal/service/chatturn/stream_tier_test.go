package chatturn

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// mapPlanResolver resolves a fixed tier per business id, so one Turn can drive
// two businesses to different tiers through the same buildOrchestratorRequest
// path.
type mapPlanResolver struct{ byID map[uuid.UUID]string }

func (m mapPlanResolver) Resolve(_ context.Context, id uuid.UUID) planresolver.Plan {
	return planresolver.Plan{Code: m.byID[id], RateLimitTier: m.byID[id]}
}

func enrichedFor(biz *domain.Business) *enrichmentResult {
	return &enrichmentResult{
		business:           biz,
		activeIntegrations: []string{},
		history:            nil,
		userMessage:        &domain.Message{ID: "msg-1"},
		project:            projectFields{allowedTools: []string{}},
		businessApprovals:  map[string]domain.ToolFloor{},
		projectOverrides:   map[string]domain.ToolFloor{},
	}
}

func turnReq() TurnRequest {
	return TurnRequest{Model: "m", Message: "hi", UserID: uuid.New(), Locale: language.Russian}
}

// TestBuildOrchestratorRequest_TierFromResolver is the fail-on-revert guard for
// the load-bearing stream.go fix: the orchestrator "tier" must come from the
// PlanResolver, NOT the legacy hardcoded "". A Pro business must forward "pro"
// — reverting stream.go to `"tier": ""` breaks this test.
func TestBuildOrchestratorRequest_TierFromResolver(t *testing.T) {
	proBiz := &domain.Business{ID: uuid.New(), Name: "Pro Co"}
	resolver := mapPlanResolver{byID: map[uuid.UUID]string{proBiz.ID: "pro"}}

	turn := &Turn{}
	turn.SetPlanResolver(resolver)

	body := turn.buildOrchestratorRequest(context.Background(), turnReq(), enrichedFor(proBiz))

	require.Equal(t, "pro", body["tier"], "tier must be the resolver's tier for a Pro business")
	require.NotEqual(t, "", body["tier"], "tier must NOT be the legacy hardcoded empty string")
}

// A Free business and a Pro business must resolve to DIFFERENT tiers through the
// same request-build path (per-business tiering, not a static config value).
func TestBuildOrchestratorRequest_FreeAndProDiffer(t *testing.T) {
	proBiz := &domain.Business{ID: uuid.New(), Name: "Pro Co"}
	freeBiz := &domain.Business{ID: uuid.New(), Name: "Free Co"}
	resolver := mapPlanResolver{byID: map[uuid.UUID]string{
		proBiz.ID:  "pro",
		freeBiz.ID: "free",
	}}

	turn := &Turn{}
	turn.SetPlanResolver(resolver)

	proBody := turn.buildOrchestratorRequest(context.Background(), turnReq(), enrichedFor(proBiz))
	freeBody := turn.buildOrchestratorRequest(context.Background(), turnReq(), enrichedFor(freeBiz))

	require.Equal(t, "pro", proBody["tier"])
	require.Equal(t, "free", freeBody["tier"])
	require.NotEqual(t, proBody["tier"], freeBody["tier"], "different plans must forward different tiers")
}

// With no resolver wired (struct-literal test path), the request forwards the
// byte-identical legacy empty tier — the behavior-preserving fallback.
func TestBuildOrchestratorRequest_NilResolverPreservesLegacyEmptyTier(t *testing.T) {
	biz := &domain.Business{ID: uuid.New(), Name: "Acme"}

	turn := &Turn{}
	body := turn.buildOrchestratorRequest(context.Background(), turnReq(), enrichedFor(biz))

	require.Equal(t, "", body["tier"], "a nil resolver must forward the legacy empty tier")
}
