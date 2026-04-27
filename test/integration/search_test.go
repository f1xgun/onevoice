package integration

import (
	"testing"
)

// Phase 19 / Plan 19-03 — search backend integration tests.
//
// Plan-mandated subtests (all scaffolded; bodies filled out in Task 4):
//
//   - TestSearchCrossTenant — User A's GET /search?q=инвойс must return ONLY
//     User A's conversations; User B's must be invisible. Symmetric. Mirrors
//     test/integration/authorization_test.go's two-user shape (lines 13-194).
//     This is the BLOCKING acceptance for T-19-CROSS-TENANT (per the plan's
//     threat_model).
//
//   - TestSearchEmptyQueryReturns400 — q="" or q < 2 chars → 400 (D-13).
//
//   - TestSearchMissingBearerReturns401 — no Authorization header → 401
//     (auth middleware rejects).
//
//   - TestSearch_503BeforeReady — readiness flag false → 503 +
//     Retry-After: 5. The flag is process-internal; an integration test
//     that boots a fresh API instance is the only natural way to exercise
//     this. If the readiness toggle isn't easily testable from integration,
//     the test is permitted to t.Skip with a TODO; the unit-test analog
//     in services/api/internal/handler/search_test.go covers the contract.
//
//   - TestSearchAggregatedShape — response is []SearchResult keyed by
//     conversationId, not raw messages. Asserts the per-conversation
//     aggregation (D-07).
//
//   - TestSearchProjectScope — ?project_id=… filters out conversations
//     in other projects (SEARCH-05).
//
// Helper to add to whichever main_test.go family file holds setupTestUser:
//   createConversationWithMessage(t, token, title, msg) string — POSTs a
//   conversation via /api/v1/conversations and inserts a single message.
//   Returns the conversation ID. Used to seed Russian-inflected content.
//
// The pattern source is test/integration/authorization_test.go lines 13-194
// (two-user setupTestUser + setupTestBusiness + bearer-token-scoped
// requests). Filled out concretely in Plan 19-03 / Task 4.

func TestSearchCrossTenant(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with two-user setup + GET /api/v1/search assertions")
}

func TestSearchEmptyQueryReturns400(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with q='' and q='a' bearer-authenticated requests")
}

func TestSearchMissingBearerReturns401(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with no-Authorization-header request")
}

func TestSearch_503BeforeReady(t *testing.T) {
	t.Skip("scaffold — readiness toggle not easily testable from integration; unit-test in services/api/internal/handler/search_test.go covers the contract")
}

func TestSearchAggregatedShape(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with seed of multi-message conversation + assertion that response is one row per conversation, not per message")
}

func TestSearchProjectScope(t *testing.T) {
	t.Skip("scaffold — implemented in Task 4 with two-project seed + ?project_id= query")
}
