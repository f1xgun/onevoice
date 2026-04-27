package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// captureLogsHere mirrors titler_test.go's captureLogs — local copy so
// search_test.go is self-contained when callers want to assert on the
// SEARCH-07 log shape.
func captureLogsHere(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })
	return buf
}

// fakeConvRepoSearch embeds domain.ConversationRepository as a NIL
// interface (titler_test.go pattern). Calling any unstubbed method
// nil-panics — intentional: surfaces unexpected repo calls loudly.
// Only SearchTitles and ScopedConversationIDs are overridden.
type fakeConvRepoSearch struct {
	domain.ConversationRepository

	mu sync.Mutex

	titlesReturn []domain.ConversationTitleHit
	titlesIDs    []string
	titlesErr    error
	titlesCalls  int

	scopedReturn []string
	scopedErr    error
	scopedCalls  int
}

func (r *fakeConvRepoSearch) SearchTitles(_ context.Context, _, _, _ string, _ *string, _ int) ([]domain.ConversationTitleHit, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.titlesCalls++
	if r.titlesErr != nil {
		return nil, nil, r.titlesErr
	}
	return r.titlesReturn, r.titlesIDs, nil
}

func (r *fakeConvRepoSearch) ScopedConversationIDs(_ context.Context, _, _ string, _ *string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopedCalls++
	if r.scopedErr != nil {
		return nil, r.scopedErr
	}
	return r.scopedReturn, nil
}

// fakeMsgRepoSearch — same nil-embedded pattern for MessageRepository.
type fakeMsgRepoSearch struct {
	domain.MessageRepository

	mu sync.Mutex

	hitsReturn []domain.MessageSearchHit
	hitsErr    error
	hitsCalls  int
}

func (r *fakeMsgRepoSearch) SearchByConversationIDs(_ context.Context, _ string, _ []string, _ int) ([]domain.MessageSearchHit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hitsCalls++
	if r.hitsErr != nil {
		return nil, r.hitsErr
	}
	return r.hitsReturn, nil
}

// TestNewSearcher_NilGuards — startup-time wiring bugs MUST surface as
// constructor panics. Mirrors TestNewTitler_NilGuards in titler_test.go.
func TestNewSearcher_NilGuards(t *testing.T) {
	t.Run("nil convRepo panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r, "expected panic")
		}()
		_ = NewSearcher(nil, &fakeMsgRepoSearch{})
	})
	t.Run("nil msgRepo panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r, "expected panic")
		}()
		_ = NewSearcher(&fakeConvRepoSearch{}, nil)
	})
}

// TestSearcher_RejectsEmptyScope — defense-in-depth (Pitfalls §19,
// T-19-CROSS-TENANT mitigation). Empty businessID OR userID returns
// ErrInvalidScope WITHOUT invoking either repo.
func TestSearcher_RejectsEmptyScope(t *testing.T) {
	cr := &fakeConvRepoSearch{}
	mr := &fakeMsgRepoSearch{}
	s := NewSearcher(cr, mr)
	s.MarkIndexesReady()

	t.Run("empty businessID", func(t *testing.T) {
		_, err := s.Search(context.Background(), "", "user-1", "anything", nil, 20)
		assert.ErrorIs(t, err, domain.ErrInvalidScope)
		assert.Equal(t, 0, cr.titlesCalls, "no repo call when scope guard trips")
		assert.Equal(t, 0, cr.scopedCalls, "no repo call when scope guard trips")
		assert.Equal(t, 0, mr.hitsCalls, "no repo call when scope guard trips")
	})
	t.Run("empty userID", func(t *testing.T) {
		_, err := s.Search(context.Background(), "biz-1", "", "anything", nil, 20)
		assert.ErrorIs(t, err, domain.ErrInvalidScope)
	})
}

// TestSearcher_RejectsBeforeIndexReady — readiness gate
// (T-19-INDEX-503 mitigation). Without MarkIndexesReady the search
// returns ErrSearchIndexNotReady; the handler maps that to 503 +
// Retry-After: 5.
func TestSearcher_RejectsBeforeIndexReady(t *testing.T) {
	cr := &fakeConvRepoSearch{}
	mr := &fakeMsgRepoSearch{}
	s := NewSearcher(cr, mr) // do NOT call MarkIndexesReady

	_, err := s.Search(context.Background(), "biz-1", "user-1", "anything", nil, 20)
	assert.ErrorIs(t, err, domain.ErrSearchIndexNotReady)
	assert.False(t, s.IsReady(), "IsReady() must be false until MarkIndexesReady is called")
}

// TestSearcher_HappyPath — happy path: title + content hits both fire,
// mergeAndRank produces one row per conversation.
func TestSearcher_HappyPath(t *testing.T) {
	cr := &fakeConvRepoSearch{
		titlesReturn: []domain.ConversationTitleHit{
			{ID: "conv-A", Title: "Запрос инвойс", Score: 1.5, BusinessID: "biz-1", UserID: "user-1"},
		},
		titlesIDs:    []string{"conv-A"},
		scopedReturn: []string{"conv-A", "conv-B"},
	}
	mr := &fakeMsgRepoSearch{
		hitsReturn: []domain.MessageSearchHit{
			{
				ConversationID: "conv-B",
				TopMessageID:   "msg-1",
				TopContent:     "Можно ли отправить инвойс по электронной почте?",
				TopScore:       1.0,
				MatchCount:     1,
			},
		},
	}
	s := NewSearcher(cr, mr)
	s.MarkIndexesReady()

	results, err := s.Search(context.Background(), "biz-1", "user-1", "инвойс", nil, 20)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byID := map[string]SearchResult{}
	for _, r := range results {
		byID[r.ConversationID] = r
	}
	require.Contains(t, byID, "conv-A")
	require.Contains(t, byID, "conv-B")

	// conv-A is title-only: title × 20 = 30, no snippet.
	convA := byID["conv-A"]
	assert.InDelta(t, 30.0, convA.Score, 0.001)
	assert.Empty(t, convA.Snippet)

	// conv-B is content-only: content × 10 = 10, snippet must be set.
	convB := byID["conv-B"]
	assert.InDelta(t, 10.0, convB.Score, 0.001)
	assert.NotEmpty(t, convB.Snippet, "content match must produce a snippet")
	assert.Equal(t, "msg-1", convB.TopMessageID)
	assert.Equal(t, 1, convB.MatchCount)

	// Sort: convA (30) before convB (10).
	assert.Equal(t, "conv-A", results[0].ConversationID)
}

// TestSearcher_TitleAndContentMerge — when the same conversation
// appears in BOTH title and content hits, the row keeps the stronger
// score and fills snippet+marks from content.
func TestSearcher_TitleAndContentMerge(t *testing.T) {
	cr := &fakeConvRepoSearch{
		titlesReturn: []domain.ConversationTitleHit{
			{ID: "conv-A", Title: "Заголовок инвойс", Score: 0.5},
		},
		titlesIDs:    []string{"conv-A"},
		scopedReturn: []string{"conv-A"},
	}
	mr := &fakeMsgRepoSearch{
		hitsReturn: []domain.MessageSearchHit{
			{
				ConversationID: "conv-A",
				TopMessageID:   "msg-1",
				TopContent:     "Тут содержание про инвойс",
				TopScore:       2.0, // 2 × 10 = 20 > title 0.5 × 20 = 10
				MatchCount:     3,
			},
		},
	}
	s := NewSearcher(cr, mr)
	s.MarkIndexesReady()

	results, err := s.Search(context.Background(), "biz-1", "user-1", "инвойс", nil, 20)
	require.NoError(t, err)
	require.Len(t, results, 1)
	r := results[0]
	assert.Equal(t, "conv-A", r.ConversationID)
	assert.InDelta(t, 20.0, r.Score, 0.001, "merged row keeps the stronger score")
	assert.Equal(t, "Заголовок инвойс", r.Title, "title preserved from title hit")
	assert.NotEmpty(t, r.Snippet, "snippet filled from content hit")
	assert.Equal(t, 3, r.MatchCount)
}

// TestSearcher_PropagatesRepoError — repo errors bubble up unchanged so
// the handler can map them.
func TestSearcher_PropagatesRepoError(t *testing.T) {
	wantErr := errors.New("mongo down")
	cr := &fakeConvRepoSearch{titlesErr: wantErr}
	mr := &fakeMsgRepoSearch{}
	s := NewSearcher(cr, mr)
	s.MarkIndexesReady()

	_, err := s.Search(context.Background(), "biz-1", "user-1", "x", nil, 20)
	assert.ErrorIs(t, err, wantErr)
}

// TestSearcher_LogShape_NoQueryText — SEARCH-07 / T-19-LOG-LEAK
// mitigation. The captured log buffer MUST contain `query_length` AND
// MUST NOT contain the literal query text bytes. This is the
// load-bearing test for the metadata-only contract.
func TestSearcher_LogShape_NoQueryText(t *testing.T) {
	buf := captureLogsHere(t)

	cr := &fakeConvRepoSearch{
		scopedReturn: []string{},
	}
	mr := &fakeMsgRepoSearch{}
	s := NewSearcher(cr, mr)
	s.MarkIndexesReady()

	const literalQuery = "конфиденциальныйпоиск42"
	_, err := s.Search(context.Background(), "biz-1", "user-1", literalQuery, nil, 20)
	require.NoError(t, err)

	logs := buf.String()
	assert.Contains(t, logs, "query_length", "metadata field present")
	assert.NotContains(t, logs, literalQuery,
		"query text leaked into logs (T-19-LOG-LEAK / SEARCH-07 violation)")
	assert.NotContains(t, logs, `"query"=`,
		"NO 'query' slog key allowed (only 'query_length')")
	assert.NotContains(t, logs, "query=конфиденциальныйпоиск42",
		"query text leaked into logs")
}
