package repository

import (
	"testing"
)

// TestSearchByConversationIDs — Plan 19-03 / Task 2 / SEARCH-03
// scaffold (Wave 0).
//
// Will be filled out in Task 2 once messageRepository.SearchByConversationIDs
// lands. The body will seed messages across two conversations, run the
// aggregation query for a Russian-stemmed term, and assert the pipeline
// returns one row per conversation (with top_message_id, top_content,
// top_score, match_count). Empty convIDs slice must return an empty
// result set without error.
//
// See .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §5 for
// the six-stage pipeline shape ($match-with-$text-first, $addFields,
// $sort, $group, $sort, $limit). Cap convIDs at 1000 (RESEARCH §15 Q10).
func TestSearchByConversationIDs(t *testing.T) {
	t.Skip("scaffold — implemented in Task 2 with messageRepository.SearchByConversationIDs")
}

// TestSearchByConversationIDs_EmptyAllowlist — same scaffold; tests that
// an empty convIDs slice returns ([], nil) without invoking Mongo.
func TestSearchByConversationIDs_EmptyAllowlist(t *testing.T) {
	t.Skip("scaffold — implemented in Task 2")
}
