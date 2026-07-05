package creditgrant

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

type fakeEnum struct {
	ids []uuid.UUID
	err error
}

func (f fakeEnum) EnumerateActiveBusinessIDs(context.Context) ([]uuid.UUID, error) {
	return f.ids, f.err
}

type grantCall struct {
	id      uuid.UUID
	credits int
	period  string
}

type fakeGranter struct {
	calls          []grantCall
	granted        map[uuid.UUID]bool
	errs           map[uuid.UUID]error
	defaultGranted bool
}

func (f *fakeGranter) GrantMonthly(_ context.Context, id uuid.UUID, credits int, period string) (bool, error) {
	f.calls = append(f.calls, grantCall{id: id, credits: credits, period: period})
	if e, ok := f.errs[id]; ok {
		return false, e
	}
	if g, ok := f.granted[id]; ok {
		return g, nil
	}
	return f.defaultGranted, nil
}

type fakeResolver struct {
	plans map[uuid.UUID]planresolver.Plan
	def   planresolver.Plan
}

func (f fakeResolver) Resolve(_ context.Context, id uuid.UUID) planresolver.Plan {
	if p, ok := f.plans[id]; ok {
		return p
	}
	return f.def
}

type fakeInvalidator struct{ ids []uuid.UUID }

func (f *fakeInvalidator) Invalidate(id uuid.UUID) { f.ids = append(f.ids, id) }

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func withFixedNow(s *Service, t time.Time) { s.now = func() time.Time { return t } }

// TestGrantAll_FreshBusinessGranted: a business on the Free plan is granted its
// monthly_credits for the current UTC period, counted, and its plan cache
// invalidated.
func TestGrantAll_FreshBusinessGranted(t *testing.T) {
	biz := uuid.New()
	granter := &fakeGranter{defaultGranted: true}
	inv := &fakeInvalidator{}
	s := New(
		fakeEnum{ids: []uuid.UUID{biz}},
		granter,
		fakeResolver{def: planresolver.Plan{Code: "free", MonthlyCredits: 100}},
		inv,
		testLogger(),
	)
	withFixedNow(s, time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC))

	n, err := s.GrantAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Len(t, granter.calls, 1)
	require.Equal(t, biz, granter.calls[0].id)
	require.Equal(t, 100, granter.calls[0].credits)
	require.Equal(t, "2026-07", granter.calls[0].period)
	require.Equal(t, []uuid.UUID{biz}, inv.ids)
}

// TestGrantAll_SkipsNonPositiveCredits: a plan with monthly_credits <= 0 (the
// hard Free fallback / unlimited) is never granted.
func TestGrantAll_SkipsNonPositiveCredits(t *testing.T) {
	biz := uuid.New()
	granter := &fakeGranter{defaultGranted: true}
	inv := &fakeInvalidator{}
	s := New(
		fakeEnum{ids: []uuid.UUID{biz}},
		granter,
		fakeResolver{def: planresolver.Plan{Code: "free", MonthlyCredits: 0}},
		inv,
		testLogger(),
	)

	n, err := s.GrantAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, granter.calls)
	require.Empty(t, inv.ids)
}

// TestGrantAll_IdempotentSecondPassReturnsZero: when the grant repo reports the
// period was already granted (did=false), the pass counts nothing and does not
// invalidate.
func TestGrantAll_IdempotentSecondPassReturnsZero(t *testing.T) {
	biz := uuid.New()
	granter := &fakeGranter{defaultGranted: false}
	inv := &fakeInvalidator{}
	s := New(
		fakeEnum{ids: []uuid.UUID{biz}},
		granter,
		fakeResolver{def: planresolver.Plan{Code: "pro", MonthlyCredits: 2000}},
		inv,
		testLogger(),
	)

	n, err := s.GrantAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Len(t, granter.calls, 1, "still attempts the grant (repo decides idempotency)")
	require.Empty(t, inv.ids)
}

// TestGrantAll_PerBusinessErrorSkipped: one business's grant write failing is
// logged and skipped; the pass continues and grants the rest.
func TestGrantAll_PerBusinessErrorSkipped(t *testing.T) {
	bad, good := uuid.New(), uuid.New()
	granter := &fakeGranter{
		defaultGranted: true,
		errs:           map[uuid.UUID]error{bad: errors.New("db down")},
	}
	inv := &fakeInvalidator{}
	s := New(
		fakeEnum{ids: []uuid.UUID{bad, good}},
		granter,
		fakeResolver{def: planresolver.Plan{Code: "free", MonthlyCredits: 100}},
		inv,
		testLogger(),
	)

	n, err := s.GrantAll(context.Background())
	require.NoError(t, err, "a single business failing must not abort the pass")
	require.Equal(t, 1, n)
	require.Equal(t, []uuid.UUID{good}, inv.ids)
}

// TestGrantAll_EnumerationErrorAborts: an enumeration failure aborts the whole
// pass with an error.
func TestGrantAll_EnumerationErrorAborts(t *testing.T) {
	granter := &fakeGranter{defaultGranted: true}
	s := New(
		fakeEnum{err: errors.New("query failed")},
		granter,
		fakeResolver{def: planresolver.Plan{MonthlyCredits: 100}},
		&fakeInvalidator{},
		testLogger(),
	)

	n, err := s.GrantAll(context.Background())
	require.Error(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, granter.calls)
}
