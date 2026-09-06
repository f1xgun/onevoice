package yandex

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

type cancellationPage struct {
	playwright.Page
	closed atomic.Int64
}

func (p *cancellationPage) Close(...playwright.PageCloseOptions) error {
	p.closed.Add(1)
	return nil
}

type cancellationContext struct {
	playwright.BrowserContext
	page    cancellationPage
	created atomic.Int64
}

func (c *cancellationContext) NewPage() (playwright.Page, error) {
	c.created.Add(1)
	return &c.page, nil
}

func TestBrowserPool_CancelQueuedPage(t *testing.T) {
	for _, name := range []string{"WithPage", "ListCompanies"} {
		t.Run(name, func(t *testing.T) {
			browserContext := &cancellationContext{}
			pc := &pooledContext{ctx: browserContext, credHash: credentialHash("[]")}
			pool := &BrowserPool{browser: &struct{ playwright.Browser }{}, contexts: map[string]*pooledContext{"org": pc}, maxIdle: defaultMaxIdle}
			entered := make(chan struct{})
			release := make(chan struct{})
			ownerDone := make(chan error, 1)
			var releaseOnce sync.Once
			defer func() {
				releaseOnce.Do(func() { close(release) })
				require.NoError(t, <-ownerDone)
			}()
			go func() {
				ownerDone <- pool.WithPage(context.Background(), "org", "[]", func(playwright.Page) error {
					close(entered)
					<-release
					return nil
				})
			}()
			<-entered
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if name == "ListCompanies" {
					_, err := pool.ForBusiness("org", "[]", "").ListCompanies(ctx)
					done <- err
					return
				}
				done <- pool.WithPage(ctx, "org", "[]", func(playwright.Page) error { return nil })
			}()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(time.Second):
				releaseOnce.Do(func() { close(release) })
				<-done
				t.Fatal("queued call did not return on deadline while owner held the page")
			}
			require.True(t, pc.busy.Load())
			require.EqualValues(t, 1, browserContext.created.Load())
			require.Zero(t, browserContext.page.closed.Load())
			releaseOnce.Do(func() { close(release) })
			require.Eventually(t, func() bool { return !pc.busy.Load() }, time.Second, time.Millisecond)
			require.EqualValues(t, 1, browserContext.page.closed.Load())
			require.NoError(t, pool.WithPage(context.Background(), "org", "[]", func(playwright.Page) error { return nil }))
			require.False(t, pc.busy.Load())
			require.EqualValues(t, 2, browserContext.created.Load())
			require.EqualValues(t, 2, browserContext.page.closed.Load())
		})
	}
}
