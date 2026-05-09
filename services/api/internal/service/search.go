// Package service — search orchestration.
//
// Searcher is the in-process two-phase query orchestrator:
//
//  1. ConversationRepository.SearchTitles — $text on conversations.title
//     scoped by (user_id, business_id, project_id?). Title hits.
//  2. ConversationRepository.ScopedConversationIDs — the broader allowlist
//     used by phase 2's $in filter (every conversation visible to the
//     scope, not just title-matching ones).
//  3. MessageRepository.SearchByConversationIDs — $text on messages.content
//     scoped by conversation_id ∈ allowlist.
//  4. mergeAndRank — fold per-conversation results, scoring
//     max(titleScore × 20, contentScore × 10), build snippet + highlight
//     marks via the snowball-based helpers in snippet.go.
//
// Three threats mitigated here:
//
//   - Cross-tenant: empty businessID/userID → ErrInvalidScope (defense-in-
//     depth alongside the repository-layer guards). Phase-2's allowlist
//     comes from ScopedConversationIDs, which itself enforces scope.
//
//   - Index-503: indexReady atomic.Bool flag. Search returns
//     ErrSearchIndexNotReady until cmd/main.go calls
//     EnsureSearchIndexes successfully and then MarkIndexesReady. Handler
//     maps the sentinel to HTTP 503 + Retry-After: 5.
//
//   - Log-leak: every slog line carries `query_length` only —
//     never the literal query text. Verified by
//     TestSearcher_LogShape_NoQueryText.
package service

import (
	"context"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Search ranking weights. Title hits outrank content hits of equal raw score
// at a 2:1 ratio — see mergeAndRank for the score formula. The numeric ratio
// must stay in step with the index-side SetWeights() in
// repository/search_indexes.go.
const (
	titleHitWeight   = 20.0
	messageHitWeight = 10.0
)

// SearchResult is the per-conversation row returned by Searcher.Search.
// JSON tags drive the GET /api/v1/search response shape consumed by the
// frontend (SidebarSearch component).
type SearchResult struct {
	ConversationID string     `json:"conversationId"`
	Title          string     `json:"title,omitempty"`
	ProjectID      *string    `json:"projectId,omitempty"`
	Snippet        string     `json:"snippet,omitempty"`
	MatchCount     int        `json:"matchCount"`
	TopMessageID   string     `json:"topMessageId,omitempty"`
	Score          float64    `json:"score"`
	Marks          [][2]int   `json:"marks,omitempty"`
	LastMessageAt  *time.Time `json:"lastMessageAt,omitempty"`
}

// Searcher orchestrates the two-phase search. Constructed once
// at startup; safe for concurrent reads.
type Searcher struct {
	convRepo   domain.ConversationRepository
	msgRepo    domain.MessageRepository
	indexReady *atomic.Bool // pointer so the *handler* shares the same flag
}

// NewSearcher constructs the Searcher. Both repos are mandatory; nil
// inputs panic loudly (constructor contract — startup-time wiring bugs
// must surface at the bind site, not at the first failed search).
//
// Pattern parallels service.NewTitler in titler.go:80-91. The
// indexReady flag starts false; cmd/main.go calls MarkIndexesReady
// AFTER repository.EnsureSearchIndexes returns nil — happens-before
// edge enforced by atomic.Bool.Store.
func NewSearcher(convRepo domain.ConversationRepository, msgRepo domain.MessageRepository) *Searcher {
	if convRepo == nil {
		panic("NewSearcher: convRepo cannot be nil")
	}
	if msgRepo == nil {
		panic("NewSearcher: msgRepo cannot be nil")
	}
	return &Searcher{
		convRepo:   convRepo,
		msgRepo:    msgRepo,
		indexReady: &atomic.Bool{},
	}
}

// MarkIndexesReady flips the readiness flag to true. MUST be called
// from cmd/main.go AFTER EnsureSearchIndexes returns nil.
//
// v20.1 note: search no longer uses $text, so a missing index can't
// surface MongoServerError any more. The flag is kept as a defensive
// "search service initialized" signal — if a future change reverts
// to $text, the gate is already wired and tested. Cost is negligible
// (one atomic load per request); benefit is a stable rollback path.
func (s *Searcher) MarkIndexesReady() { s.indexReady.Store(true) }

// IsReady reports whether MarkIndexesReady has been called. Used by
// the handler readiness probe and by tests. Pure read; thread-safe.
func (s *Searcher) IsReady() bool { return s.indexReady.Load() }

// Search runs the two-phase query for the (businessID, userID) scope
// (and optional projectID), enforces defense-in-depth scope guards and
// the readiness gate, and returns up to `limit` ranked SearchResult
// rows (one per matching conversation).
//
// Cross-tenant defense:
// empty businessID OR userID → domain.ErrInvalidScope, no repo calls.
// Repository-layer methods independently enforce the same guard.
//
// Readiness gate: indexReady.Load() == false →
// domain.ErrSearchIndexNotReady. Handler maps to 503 + Retry-After: 5.
//
// Log shape: the single InfoContext line
// carries {user_id, business_id, query_length}. NEVER the literal
// query text. NO `"query"` slog key anywhere in this file.
func (s *Searcher) Search(
	ctx context.Context,
	businessID, userID, query string,
	projectID *string,
	limit int,
) ([]SearchResult, error) {
	// Defense-in-depth.
	if businessID == "" || userID == "" {
		return nil, domain.ErrInvalidScope
	}
	if !s.indexReady.Load() {
		return nil, domain.ErrSearchIndexNotReady
	}
	if limit <= 0 {
		limit = 20
	}

	// Metadata-only log line — NO query text.
	slog.InfoContext(ctx, "search.query",
		"user_id", userID,
		"business_id", businessID,
		"query_length", len(query),
	)

	titleHits, _, err := s.convRepo.SearchTitles(ctx, businessID, userID, query, projectID, limit)
	if err != nil {
		return nil, err
	}
	scopedIDs, err := s.convRepo.ScopedConversationIDs(ctx, businessID, userID, projectID)
	if err != nil {
		return nil, err
	}
	msgHits, err := s.msgRepo.SearchByConversationIDs(ctx, query, scopedIDs, limit*2)
	if err != nil {
		return nil, err
	}

	prefixes := QueryPrefixes(query)
	merged := mergeAndRank(titleHits, msgHits, titleHitWeight, messageHitWeight, limit, prefixes)
	return merged, nil
}

// mergeAndRank combines title + content hits into per-conversation rows.
// Score formula: max(titleScore × titleW, contentScore × contentW).
// Title matches outrank content matches of equal raw score; strong
// content matches still surface. Snippet + highlight marks come from
// the top-scoring content message via BuildSnippet + HighlightRanges.
func mergeAndRank(
	titleHits []domain.ConversationTitleHit,
	msgHits []domain.MessageSearchHit,
	titleW, contentW float64,
	limit int,
	prefixes map[string]struct{},
) []SearchResult {
	byID := make(map[string]*SearchResult)
	for _, t := range titleHits {
		byID[t.ID] = &SearchResult{
			ConversationID: t.ID,
			Title:          t.Title,
			ProjectID:      t.ProjectID,
			Score:          t.Score * titleW,
			LastMessageAt:  t.LastMessageAt,
		}
	}
	for _, m := range msgHits {
		score := m.TopScore * contentW
		snippet := BuildSnippet(m.TopContent, prefixes)
		marks := HighlightRanges(snippet, prefixes)
		if existing, ok := byID[m.ConversationID]; ok {
			// Title and content both hit; keep the stronger score and
			// fill snippet/marks/match_count from the content side.
			if score > existing.Score {
				existing.Score = score
			}
			existing.Snippet = snippet
			existing.MatchCount = m.MatchCount
			existing.TopMessageID = m.TopMessageID
			existing.Marks = marks
		} else {
			byID[m.ConversationID] = &SearchResult{
				ConversationID: m.ConversationID,
				Score:          score,
				Snippet:        snippet,
				MatchCount:     m.MatchCount,
				TopMessageID:   m.TopMessageID,
				Marks:          marks,
			}
		}
	}
	out := make([]SearchResult, 0, len(byID))
	for _, v := range byID {
		out = append(out, *v)
	}
	// v20.1 ranking: score desc, then last_message_at desc (recency), then
	// conversation_id asc as a final deterministic tiebreaker. Without
	// these tertiary keys, equal-scoring rows ride on Go's
	// non-deterministic map iteration order through sort.Slice — so the
	// SAME query could return rows in different orders across calls.
	// SliceStable + a fully ordered comparator removes the wobble.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		ai, aj := out[i].LastMessageAt, out[j].LastMessageAt
		switch {
		case ai != nil && aj != nil && !ai.Equal(*aj):
			return ai.After(*aj)
		case ai != nil && aj == nil:
			return true
		case ai == nil && aj != nil:
			return false
		}
		return out[i].ConversationID < out[j].ConversationID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
