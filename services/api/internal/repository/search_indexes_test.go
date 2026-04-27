package repository

import (
	"testing"
)

// TestEnsureSearchIndexes_Idempotent — Plan 19-03 / Task 2 / SEARCH-01
// scaffold (Wave 0).
//
// Will be filled out in Task 2 once EnsureSearchIndexes lands. The body
// will:
//  1. Drop any pre-existing text indexes for a clean-slate cold boot.
//  2. Call EnsureSearchIndexes twice and assert nil error each time
//     (idempotency — Mongo CreateOne with stable spec is a no-op).
//  3. List specifications on the conversations + messages collections
//     and assert both `conversations_title_text_v19` and
//     `messages_content_text_v19` are present.
//
// See .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §4 for
// the index option recipe (SetDefaultLanguage("russian"), SetWeights, etc.).
func TestEnsureSearchIndexes_Idempotent(t *testing.T) {
	t.Skip("scaffold — implemented in Task 2 with EnsureSearchIndexes")
}
