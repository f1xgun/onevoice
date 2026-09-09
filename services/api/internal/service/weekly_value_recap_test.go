package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type recapRepoFake struct {
	ids               []uuid.UUID
	rows              map[uuid.UUID]domain.WeeklyValueRecap
	writes            []domain.WeeklyValueRecap
	err               error
	upsertErrors      map[uuid.UUID]error
	sawUpsertDeadline bool
}

func (f *recapRepoFake) EnumerateActiveBusinessIDs(context.Context) ([]uuid.UUID, error) {
	return f.ids, f.err
}
func (f *recapRepoFake) Upsert(ctx context.Context, v domain.WeeklyValueRecap) error {
	_, f.sawUpsertDeadline = ctx.Deadline()
	if err := f.upsertErrors[v.BusinessID]; err != nil {
		return err
	}
	f.writes = append(f.writes, v)
	return nil
}
func (f *recapRepoFake) GetForWeek(_ context.Context, id uuid.UUID, week time.Time) (*domain.WeeklyValueRecap, error) {
	v, ok := f.rows[id]
	if !ok || !v.WeekStart.Equal(week) {
		return nil, nil
	}
	return &v, nil
}

type recapSourceFake struct {
	values   map[uuid.UUID]domain.WeeklyValueRecap
	failures map[uuid.UUID]error
	windows  [][2]time.Time
}

func (f *recapSourceFake) CountWeeklyValue(_ context.Context, id uuid.UUID, from, to time.Time) (domain.WeeklyValueRecap, error) {
	f.windows = append(f.windows, [2]time.Time{from, to})
	if err := f.failures[id]; err != nil {
		return domain.WeeklyValueRecap{}, err
	}
	v := f.values[id]
	v.BusinessID = id
	v.WeekStart = from
	v.WeekEnd = to
	return v, nil
}

func TestPreviousCompletedUTCWeekBoundaries(t *testing.T) {
	from, to := PreviousCompletedUTCWeek(time.Date(2026, 9, 9, 18, 0, 0, 0, time.FixedZone("x", 3*3600)))
	assert.Equal(t, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), from)
	assert.Equal(t, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), to)
	from, to = PreviousCompletedUTCWeek(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), from)
	assert.Equal(t, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), to)
}

func TestWeeklyValueRecapSweepSuppressesLowAndIsolatesFailures(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo := &recapRepoFake{ids: []uuid.UUID{a, b, c}, upsertErrors: map[uuid.UUID]error{c: errors.New("persist failure")}}
	source := &recapSourceFake{values: map[uuid.UUID]domain.WeeklyValueRecap{a: {PublishedPosts: 1}, c: {PublishedPosts: 1, CompletedSyncs: 1}}, failures: map[uuid.UUID]error{b: errors.New("tenant failure")}}
	n, err := NewWeeklyValueRecapService(repo, source).Sweep(context.Background(), time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	require.Error(t, err, "partial tenant failure must keep the worker pass from reporting success")
	assert.Equal(t, 1, n)
	require.Len(t, repo.writes, 1)
	assert.Equal(t, a, repo.writes[0].BusinessID, "low counts must overwrite any previously visible row")
	assert.True(t, repo.sawUpsertDeadline, "the per-organization deadline must cover persistence")
}

func TestWeeklyValueRecapLatestRequiresExactPriorWeekAndThreshold(t *testing.T) {
	id := uuid.New()
	week, _ := PreviousCompletedUTCWeek(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	repo := &recapRepoFake{rows: map[uuid.UUID]domain.WeeklyValueRecap{id: {WeekStart: week, PublishedPosts: 1}}}
	svc := NewWeeklyValueRecapService(repo, &recapSourceFake{})
	v, err := svc.Latest(context.Background(), id, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Nil(t, v)
	repo.rows[id] = domain.WeeklyValueRecap{WeekStart: week, PublishedPosts: 2}
	v, err = svc.Latest(context.Background(), id, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, 2, v.Total())
}
