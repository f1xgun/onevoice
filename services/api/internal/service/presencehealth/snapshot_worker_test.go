package presencehealth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type stubEnum struct {
	ids []uuid.UUID
	err error
}

func (s stubEnum) EnumerateActiveBusinessIDs(context.Context) ([]uuid.UUID, error) {
	return s.ids, s.err
}

// stubScorer returns a per-business composite (nil composite when the id is in
// skipComposite; an error when in errIDs).
type stubScorer struct {
	composite     int
	skipComposite map[uuid.UUID]bool
	errIDs        map[uuid.UUID]bool
}

func (s stubScorer) Score(_ context.Context, id uuid.UUID, _ int) (service.PresenceHealthScore, error) {
	if s.errIDs[id] {
		return service.PresenceHealthScore{}, errors.New("score boom")
	}
	if s.skipComposite[id] {
		return service.PresenceHealthScore{}, nil // nil composite → empty presence
	}
	c := s.composite
	return service.PresenceHealthScore{Composite: &c}, nil
}

type recordingUpserter struct {
	stamped []domain.PresenceHealthSnapshot
	err     error
}

func (r *recordingUpserter) Upsert(_ context.Context, snap domain.PresenceHealthSnapshot) error {
	if r.err != nil {
		return r.err
	}
	r.stamped = append(r.stamped, snap)
	return nil
}

// TestISOWeekKey_Format pins the 'YYYY-Www' key so the lexical prior-week
// ordering the trend depends on stays stable.
func TestISOWeekKey_Format(t *testing.T) {
	// 2026-01-05 is the Monday of ISO week 2 of 2026.
	got := ISOWeekKey(time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, "2026-W02", got)

	// A single-digit week is zero-padded so the string sorts correctly.
	assert.Equal(t, "2026-W01", ISOWeekKey(time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)))
}

// TestSnapshotAll_StampsEachActiveBusiness proves one snapshot per active
// business is stamped with the current ISO-week key and the composite score.
func TestSnapshotAll_StampsEachActiveBusiness(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	up := &recordingUpserter{}
	svc := New(stubEnum{ids: ids}, stubScorer{composite: 77}, up, testLogger())

	n, err := svc.SnapshotAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, up.stamped, 2)

	week := ISOWeekKey(time.Now().UTC())
	for _, snap := range up.stamped {
		assert.Equal(t, week, snap.ISOWeek)
		assert.Equal(t, 77, snap.Composite)
		assert.NotEqual(t, uuid.Nil, snap.ID)
	}
}

// TestSnapshotAll_SkipsEmptyPresence proves a business whose score has no
// composite (no reviews yet) is skipped — no snapshot row is written for it.
func TestSnapshotAll_SkipsEmptyPresence(t *testing.T) {
	empty := uuid.New()
	scored := uuid.New()
	up := &recordingUpserter{}
	svc := New(
		stubEnum{ids: []uuid.UUID{empty, scored}},
		stubScorer{composite: 50, skipComposite: map[uuid.UUID]bool{empty: true}},
		up,
		testLogger(),
	)

	n, err := svc.SnapshotAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, up.stamped, 1)
	assert.Equal(t, scored, up.stamped[0].BusinessID)
}

// TestSnapshotAll_PerBusinessErrorSkipped proves a single business's score
// failure is skipped so the rest of the fleet still gets stamped (only
// enumeration failure aborts).
func TestSnapshotAll_PerBusinessErrorSkipped(t *testing.T) {
	bad := uuid.New()
	good := uuid.New()
	up := &recordingUpserter{}
	svc := New(
		stubEnum{ids: []uuid.UUID{bad, good}},
		stubScorer{composite: 60, errIDs: map[uuid.UUID]bool{bad: true}},
		up,
		testLogger(),
	)

	n, err := svc.SnapshotAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, up.stamped, 1)
	assert.Equal(t, good, up.stamped[0].BusinessID)
}

// TestSnapshotAll_EnumerationErrorAborts proves an enumeration failure aborts
// the whole pass with an error and stamps nothing.
func TestSnapshotAll_EnumerationErrorAborts(t *testing.T) {
	up := &recordingUpserter{}
	svc := New(stubEnum{err: errors.New("enum boom")}, stubScorer{composite: 50}, up, testLogger())

	n, err := svc.SnapshotAll(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, up.stamped)
}

// TestSnapshotAll_ContextCancelStops proves a canceled context stops the pass
// mid-fleet rather than continuing to enumerate-and-stamp.
func TestSnapshotAll_ContextCancelStops(t *testing.T) {
	up := &recordingUpserter{}
	svc := New(
		stubEnum{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		stubScorer{composite: 50},
		up,
		testLogger(),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.SnapshotAll(ctx)
	require.Error(t, err)
	assert.Empty(t, up.stamped, "no business should be stamped once the context is canceled")
}

// TestSnapshotFromScore_PreservesNullableSubScores proves an absent sync
// sub-score is preserved as NULL (nil pointer) while absent review sub-scores
// default to 0 in the stored row (the columns are NOT NULL).
func TestSnapshotFromScore_PreservesNullableSubScores(t *testing.T) {
	biz := uuid.New()
	c := 90
	rating := 100
	// No SLA/Coverage/Sync sub-scores (all nil).
	score := service.PresenceHealthScore{
		Composite: &c,
		SubScores: service.PresenceSubScores{Rating: &rating},
	}

	snap := SnapshotFromScore(biz, "2026-W10", score, time.Now())

	assert.Equal(t, 90, snap.Composite)
	assert.Equal(t, 100, snap.RatingScore)
	assert.Equal(t, 0, snap.SLAScore, "absent SLA sub-score stored as 0 (NOT NULL column)")
	assert.Equal(t, 0, snap.CoverageScore)
	assert.Nil(t, snap.SyncScore, "absent sync sub-score preserved as NULL")
}
