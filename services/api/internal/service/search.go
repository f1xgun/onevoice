// Package service — search orchestration. See docs/services/search.md.
package service

import (
	"context"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Search ranking weights — keep the 2:1 ratio in step with repository/search_indexes.go SetWeights().
const (
	titleHitWeight   = 20.0
	messageHitWeight = 10.0
)

// SearchResult is the per-conversation row returned by Searcher.Search. JSON tags drive GET /api/v1/search.
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

// Searcher orchestrates the two-phase search. Safe for concurrent reads.
type Searcher struct {
	convRepo   domain.ConversationRepository
	msgRepo    domain.MessageRepository
	indexReady *atomic.Bool // pointer so handlers share the same flag instance
}

// NewSearcher constructs the Searcher; nil repos panic (wiring bugs must surface at bind site).
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

// MarkIndexesReady flips the readiness flag — MUST run only after EnsureSearchIndexes returns nil.
func (s *Searcher) MarkIndexesReady() { s.indexReady.Store(true) }

// IsReady reports whether MarkIndexesReady has been called. Thread-safe.
func (s *Searcher) IsReady() bool { return s.indexReady.Load() }

// Search runs the two-phase query for (businessID, userID, projectID?). See docs/services/search.md.
func (s *Searcher) Search(
	ctx context.Context,
	businessID, userID, query string,
	projectID *string,
	limit int,
) ([]SearchResult, error) {
	// Cross-tenant defense-in-depth — repository layer enforces the same guard.
	if businessID == "" || userID == "" {
		return nil, domain.ErrInvalidScope
	}
	if !s.indexReady.Load() {
		return nil, domain.ErrSearchIndexNotReady
	}
	if limit <= 0 {
		limit = 20
	}

	// Metadata-only log line — NEVER the literal query text. Verified by TestSearcher_LogShape_NoQueryText.
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

// mergeAndRank combines title + content hits into per-conversation rows. See docs/services/search.md.
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
			// Title+content both hit: keep stronger score; fill snippet/marks/match_count from content.
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
	// Fully-ordered comparator removes the same-query wobble from Go map iteration order.
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
