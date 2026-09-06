package yandex

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

type blockingCompaniesPage struct {
	playwright.Page
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (p *blockingCompaniesPage) Goto(string, ...playwright.PageGotoOptions) (playwright.Response, error) {
	close(p.started)
	<-p.closed
	return nil, errors.New("page closed")
}
func (p *blockingCompaniesPage) Close(...playwright.PageCloseOptions) error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func TestListCompanies_CancellationClosesNavigation(t *testing.T) {
	page := &blockingCompaniesPage{started: make(chan struct{}), closed: make(chan struct{})}
	pool := newMockBrowserPool(nil)
	pool.withPageFn = func(_ context.Context, _, _ string, fn func(playwright.Page) error) error { return fn(page) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := pool.ForBusiness("test", "[]", "default").ListCompanies(ctx); done <- err }()
	select {
	case <-page.started:
	case <-time.After(time.Second):
		t.Fatal("navigation did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled navigation kept running")
	}
}

func TestWithRetry_CancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	started := time.Now()
	err := withRetry(ctx, 2, func() error { calls++; cancel(); return errors.New("navigation failed") })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}
