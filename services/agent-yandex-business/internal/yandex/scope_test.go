package yandex

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/playwright-community/playwright-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

func TestScopeAllowed(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"BusinessYandexRu", "business.yandex.ru", true},
		{"BusinessYandexRuSubpath", "sprav.business.yandex.ru", true},
		{"BusinessYandexRuNestedSub", "api.sprav.business.yandex.ru", true},
		{"YaStaticNet", "yastatic.net", true},
		{"YaStaticNetSub", "cdn.yastatic.net", true},
		{"PassportYandexRu", "passport.yandex.ru", true},
		{"DeniedMail", "mail.yandex.ru", false},
		{"DeniedDisk", "disk.yandex.ru", false},
		{"DeniedMoney", "money.yandex.ru", false},
		{"DeniedMarket", "market.yandex.ru", false},
		{"DeniedMC", "mc.yandex.ru", false},
		{"DeniedYandexRuRoot", "yandex.ru", false},
		{"DeniedUnknown", "example.com", false},
		{"CaseInsensitive", "Business.Yandex.Ru", true},
		{"EmptyDenied", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, scopeAllowed(tt.host))
		})
	}
}

func TestMetricsCounterRegistered(t *testing.T) {
	metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru").Inc()
	count := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))
	assert.Greater(t, count, 0.0)
}

// fakeRequest satisfies playwright.Request; only URL() is used by installScopeGate.
type fakeRequest struct{ url string }

func (r *fakeRequest) URL() string                                      { return r.url }
func (r *fakeRequest) Method() string                                   { return "GET" }
func (r *fakeRequest) Headers() map[string]string                       { return nil }
func (r *fakeRequest) HeaderValue(string) (string, error)               { return "", nil }
func (r *fakeRequest) AllHeaders() (map[string]string, error)           { return nil, nil }
func (r *fakeRequest) HeadersArray() ([]playwright.NameValue, error)    { return nil, nil }
func (r *fakeRequest) PostData() (string, error)                        { return "", nil }
func (r *fakeRequest) PostDataBuffer() ([]byte, error)                  { return nil, nil }
func (r *fakeRequest) PostDataJSON(interface{}) error                   { return nil }
func (r *fakeRequest) ResourceType() string                             { return "" }
func (r *fakeRequest) Frame() playwright.Frame                          { return nil }
func (r *fakeRequest) IsNavigationRequest() bool                        { return false }
func (r *fakeRequest) RedirectedFrom() playwright.Request               { return nil }
func (r *fakeRequest) RedirectedTo() playwright.Request                 { return nil }
func (r *fakeRequest) Failure() error                                   { return nil }
func (r *fakeRequest) Response() (playwright.Response, error)           { return nil, nil }
func (r *fakeRequest) Timing() *playwright.RequestTiming                { return nil }
func (r *fakeRequest) Sizes() (*playwright.RequestSizesResult, error)   { return nil, nil }

// fakeRoute satisfies playwright.Route and records terminal-action calls.
type fakeRoute struct {
	continued atomic.Bool
	aborted   atomic.Bool
	abortCode atomic.Value
	reqURL    string
}

func (f *fakeRoute) Continue(...playwright.RouteContinueOptions) error { f.continued.Store(true); return nil }
func (f *fakeRoute) Abort(code ...string) error {
	f.aborted.Store(true)
	if len(code) > 0 {
		f.abortCode.Store(code[0])
	}
	return nil
}
func (f *fakeRoute) Fallback(...playwright.RouteFallbackOptions) error           { return nil }
func (f *fakeRoute) Fulfill(...playwright.RouteFulfillOptions) error             { return nil }
func (f *fakeRoute) Fetch(...playwright.RouteFetchOptions) (playwright.APIResponse, error) {
	return nil, nil
}
func (f *fakeRoute) Request() playwright.Request { return &fakeRequest{url: f.reqURL} }

// fakeBrowserContext captures the Route handler registered by installScopeGate.
type fakeBrowserContext struct {
	handler func(playwright.Route)
}

func (f *fakeBrowserContext) Route(_ interface{}, handler func(playwright.Route), _ ...int) error {
	f.handler = handler
	return nil
}

func TestInstallScopeGate_AllowPath(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	err := installScopeGate(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default())
	require.NoError(t, err)
	require.NotNil(t, bCtx.handler)

	route := &fakeRoute{reqURL: "https://business.yandex.ru/dashboard"}
	bCtx.handler(route)

	assert.True(t, route.continued.Load(), "allowed host should call route.Continue()")
	assert.False(t, route.aborted.Load())
}

func TestInstallScopeGate_DenyPath(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	err := installScopeGate(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default())
	require.NoError(t, err)
	require.NotNil(t, bCtx.handler)

	before := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))
	route := &fakeRoute{reqURL: "https://mail.yandex.ru/inbox"}
	bCtx.handler(route)

	assert.False(t, route.continued.Load(), "denied host should not call route.Continue()")
	assert.True(t, route.aborted.Load(), "denied host should call route.Abort()")
	code, _ := route.abortCode.Load().(string)
	assert.Equal(t, "blockedbyclient", code)
	after := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))
	assert.Greater(t, after, before, "metrics counter should increment on deny")
}

func TestInstallScopeGate_ParseFailAborts(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	err := installScopeGate(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default())
	require.NoError(t, err)

	route := &fakeRoute{reqURL: "://bad-url"}
	bCtx.handler(route)

	assert.True(t, route.aborted.Load(), "parse failure should abort the route")
	assert.False(t, route.continued.Load())
}
