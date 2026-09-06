package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type landingStore struct {
	events  []repository.LandingEventRow
	signups []repository.WaitlistSignupRow
}

func (s *landingStore) InsertWaitlist(_ context.Context, row repository.WaitlistSignupRow) error {
	s.signups = append(s.signups, row)
	return nil
}
func (s *landingStore) InsertChannelVote(context.Context, repository.ChannelVoteRow) error {
	return nil
}
func (s *landingStore) InsertLandingEvent(_ context.Context, row repository.LandingEventRow) error {
	s.events = append(s.events, row)
	return nil
}

func landingRouter(t *testing.T, store *landingStore, limit int) http.Handler {
	t.Helper()
	client, _ := setupRouterTestRedis(t)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	handlers := buildTestHandlers()
	handlers.Landing = handler.NewLandingHandler(service.NewLandingService(store))
	return router.Setup(handlers, routerTestJWTSecret, client, health.New(), nil,
		router.RateLimits{Register: limit}, authz.NewCacheForTest(&fakeLoader{}, time.Second, time.Second), nil, nil, nil)
}

func TestLandingEventsAnonymousValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"anonymous", `{"cta":"hero-register","path":"/ru"}`, 204},
		{"invalid CTA", `{"cta":"unknown","path":"/"}`, 400},
		{"missing CTA", `{"path":"/"}`, 400},
		{"invalid path", `{"cta":"hero-register","path":"relative"}`, 400},
		{"long path", `{"cta":"hero-register","path":"/` + strings.Repeat("x", 1024) + `"}`, 400},
		{"long body", `{"cta":"hero-register","path":"/","extra":"` + strings.Repeat("x", 4096) + `"}`, 400},
		{"long trailing whitespace", `{"cta":"hero-register","path":"/"}` + strings.Repeat(" ", 4096), 400},
		{"second JSON", `{"cta":"hero-register","path":"/"}{}`, 400},
		{"malformed", `{`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &landingStore{}
			r := landingRouter(t, store, 10)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-events", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			require.Equal(t, tc.status, rr.Code, rr.Body.String())
			if tc.status == 204 {
				require.Equal(t, []repository.LandingEventRow{{CTA: "hero-register", Path: "/ru"}}, store.events)
			} else {
				require.Empty(t, store.events)
			}
		})
	}
}

func TestLandingEventsRateLimitByIP(t *testing.T) {
	store := &landingStore{}
	r := landingRouter(t, store, 1)
	for _, tc := range []struct {
		ip     string
		status int
	}{{"192.0.2.1:1234", 204}, {"192.0.2.1:5678", 429}, {"192.0.2.2:1234", 204}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/landing-events", strings.NewReader(`{"cta":"nav-login","path":"/"}`))
		req.RemoteAddr = tc.ip
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, tc.status, rr.Code, rr.Body.String())
		if tc.status == 429 {
			assert.NotEmpty(t, rr.Header().Get("Retry-After"))
		}
	}
	require.Len(t, store.events, 2)
}

func TestWaitlistAnonymousAttribution(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		status       int
	}{
		{"legacy", `"consent":true`, 204},
		{"billing", `"consent":true,"source":"billing","plan":"pro"`, 204},
		{"business-limit", `"consent":true,"source":"business-limit","plan":"pro"`, 204},
		{"consent required", `"consent":false,"source":"billing","plan":"pro"`, 400},
		{"invalid source", `"consent":true,"source":"unknown"`, 400},
		{"invalid plan", `"consent":true,"plan":"free"`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &landingStore{}
			r := landingRouter(t, store, 10)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", strings.NewReader(`{"email":"owner@example.org",`+tc.fields+`}`))
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			require.Equal(t, tc.status, rr.Code, rr.Body.String())
			if tc.status != 204 {
				require.Empty(t, store.signups)
				return
			}
			require.Len(t, store.signups, 1)
			if tc.name != "legacy" {
				require.NotNil(t, store.signups[0].Source)
				assert.Equal(t, tc.name, *store.signups[0].Source)
				assert.Equal(t, "pro", *store.signups[0].Plan)
			}
		})
	}
}
