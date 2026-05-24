package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// passThroughMiddleware is a no-op middleware used by the soft-restrict
// wrappers when the caller passes a nil UserLookup (legacy / test deploys
// that don't wire Phase 21-03 yet). Preserves existing behavior in those
// environments while keeping the route declarations uniform.
func passThroughMiddleware(next http.Handler) http.Handler { return next }

// Per-endpoint per-window rate limits (window is always time.Minute today).
// Register is tighter than login because automated signup abuse is the
// primary cost of getting these wrong; login/refresh are looser to absorb
// retry storms after backend hiccups. Chat / HITL share a budget because
// the chat-proxy fans out into HITL resolve calls under the same auth.
//
// Defaults are surfaced as env vars (RATE_LIMIT_REGISTER, RATE_LIMIT_LOGIN,
// RATE_LIMIT_CHAT, RATE_LIMIT_HITL); the caller of Setup passes a
// RateLimits struct constructed from those values.
const corsMaxAge = 300

// RateLimits aggregates the per-endpoint per-minute request budgets so
// router.Setup takes one parameter instead of four.
type RateLimits struct {
	Register int
	Login    int
	Chat     int
	HITL     int
}

// Handlers encapsulates all HTTP handlers
type Handlers struct {
	Auth          *handler.AuthHandler
	Business      *handler.BusinessHandler
	Integration   *handler.IntegrationHandler
	Conversation  *handler.ConversationHandler
	OAuth         *oauth.OAuthHandler     // true OAuth code-flow (VK / Yandex / Google)
	Connect       *connect.ConnectHandler // paste-flow integrations (Telegram bot-token, VK community access token)
	InternalToken *handler.InternalTokenHandler
	ChatProxy     *handler.ChatProxyHandler
	Review        *handler.ReviewHandler
	Post          *handler.PostHandler
	AgentTask     *handler.AgentTaskHandler
	Telemetry     *handler.TelemetryHandler
	Project       *handler.ProjectHandler
	HITL          *handler.HITLHandler        // resolve + resume + GET /tools
	Titler        *handler.TitlerHandler      // POST /conversations/{id}/regenerate-title
	Search        *handler.SearchHandler      // GET /api/v1/search
	Platforms     *handler.PlatformsHandler   // Public platform registry
	Permissions   *handler.PermissionsHandler // RBAC: static permission registry
	Members       *handler.MembersHandler     // RBAC: member management
	Roles         *handler.RolesHandler       // RBAC: role listing
	Invitations   *handler.InvitationsHandler // RBAC: invitation lifecycle (Phase 3)
	AuditLog      *handler.AuditLogHandler    // Phase 19 Wave 5: audit-log read endpoint
}

// Setup creates and configures the Chi router with all routes and middleware.
// allowedOrigins is the CORS whitelist sourced from CORS_ALLOWED_ORIGINS;
// callers MUST supply at least one entry so the public frontend origin can be
// configured per-environment without rebuilding the binary.
// rateLimits controls per-endpoint per-minute request budgets — see the
// RateLimits comment block above for the role of each field.
// authzCache backs the RequireBusinessAccess middleware that gates the
// /businesses/{id}/... subtree (Phase 2 v2.0 RBAC).
// users is the UserLookup the Phase 21-03 soft-restrict middleware reads
// email_verified from on every protected request (D-26..D-29 / ACCT-02).
// May be nil — when nil, the soft-restrict decorators degrade to
// pass-through (legacy/test compat).
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits, authzCache *authz.Cache, users middleware.UserLookup) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Accept-Language", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Correlation-ID"},
		AllowCredentials: true,
		MaxAge:           corsMaxAge,
	}))
	// LocaleResolver runs after CORS/correlation but before SecurityHeaders /
	// metrics / auth so even unauthenticated error responses can be localized
	// off the Accept-Language header. See pkg/i18n + Phase A1 of
	// `.planning/i18n-readiness/PLAN.md`.
	r.Use(i18n.LocaleMiddleware)
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		// Phase 21b — Password reset (ACCT-01).
		// No middleware.RateLimit chi wrapper: the handler does its own
		// per-email Redis rate-limit inside the service so the
		// timing-parity contract (CONTEXT D-15) is enforced uniformly.
		// A chi RateLimit here would short-circuit before the service
		// runs and skew the unknown-email branch.
		//
		// NO GET handler for /auth/password-reset/confirm — the frontend
		// renders the page off the ?token=… query string and the user
		// must explicitly POST after clicking the reveal CTA. This is
		// the scanner-protection (PITFALLS §1.5) — Outlook Safe Links
		// and Yandex 360 link prefetch cannot consume the token via GET.
		r.Post("/auth/password-reset/request", handlers.Auth.RequestPasswordReset)
		r.Post("/auth/password-reset/confirm", handlers.Auth.ConfirmPasswordReset)

		// Phase 21-03 — Email verification confirm (ACCT-02 / D-22, D-23).
		// PUBLIC: no JWT required. The verify-email page in the FE is
		// reachable when the user is logged out (e.g. clicks the link in
		// another browser). Returns 204 with NO Set-Cookie / session
		// material — T-VE-02 mitigation. NO GET handler — the FE renders
		// a button-gated page off the ?token=… query string and POSTs
		// only after the user clicks (scanner-protection per D-22).
		r.Post("/auth/verify-email/confirm", handlers.Auth.VerifyConfirm)

		// OAuth callback routes (public — state parameter validates session)
		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
		r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
		r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)
		r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)

		// Platform registry — single source of truth for the integration list.
		// Public so the marketing landing page can render it without auth.
		// Returns only non-sensitive metadata (name, description, status).
		r.Get("/platforms", handlers.Platforms.List)

		// Phase 3: public invitation preview — token IS the auth (CONTEXT D-04).
		// Rate-limited per-IP with the Login budget — same threat model as login
		// (automated abuse with no auth state). T-03-09 mitigation.
		if handlers.Invitations != nil {
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Get("/invitations/{token}", handlers.Invitations.Preview)
		}

		// Protected routes (require auth)
		r.Group(func(r chi.Router) {
			// Auth middleware
			r.Use(middleware.Auth(jwtSecret))

			// Auth-only routes (not business-scoped).
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)
			r.Put("/auth/password", handlers.Auth.ChangePassword)

			// Phase 21-03 — Email verification resend + email-before-verify
			// (ACCT-02 / D-21, D-24). JWT-required but NOT decorated by
			// RequireVerifiedEmail* — the whole point is they're reachable
			// to UNVERIFIED users (otherwise the user could never recover
			// from a dead email-on-file). D-30 explicitly exempts these.
			r.Post("/auth/verify-email/resend", handlers.Auth.VerifyResend)
			r.Patch("/auth/email-before-verify", handlers.Auth.EmailBeforeVerify)
			// i18n Phase A3: persist the user's UI language choice
			// ('ru'|'en'). Sits next to /auth/me/password as a sibling
			// account-self-service endpoint; the frontend syncs the cookie ↔
			// DB on login via GET /auth/me reading preferred_locale + this
			// PATCH writing it. PATCH (not PUT) because the request is a
			// partial — only the locale field — and PATCH is the verb the
			// rest of the API uses for scalar mutations (e.g.
			// /members/{userId}).
			r.Patch("/auth/locale", handlers.Auth.UpdatePreferredLocale)

			// Phase 1 v2.0 RBAC (AUTHZ-01 / CONTEXT D-15): static permission registry.
			// Auth-required (any logged-in user) — no business scope.
			if handlers.Permissions != nil {
				r.Get("/permissions", handlers.Permissions.List)
			}

			// BIZ-02 + BIZ-03 (auth-only, NOT business-scoped).
			r.Get("/businesses", handlers.Business.ListUserBusinesses)
			// Phase 21-03 (ACCT-02 / D-28): POST /businesses is gated by the
			// day-7 soft-restrict — unverified users get 7 days to convert
			// before business creation is blocked. Listing remains open
			// (read endpoints are banner-only per D-29).
			if users != nil {
				r.With(middleware.RequireVerifiedEmailDay7(users)).
					Post("/businesses", handlers.Business.CreateBusiness)
			} else {
				r.Post("/businesses", handlers.Business.CreateBusiness)
			}

			// Phase 19 manual review refresh — kicks the cross-business sync;
			// no business scope so any authed user can trigger it.
			r.Post("/reviews/refresh", handlers.Review.RefreshReviews)

			r.Post("/telemetry", handlers.Telemetry.Ingest)

			// Phase 3: invitation accept — auth-required, NOT business-scoped
			// (the {token} URL param targets a specific business inside the
			// invitation row). Rate-limited per-user with Login budget for
			// defense-in-depth (RESEARCH OQ-06). T-03-09 / T-03-10 mitigation.
			if handlers.Invitations != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Login, time.Minute, "invite_accept")).
					Post("/invitations/{token}/accept", handlers.Invitations.Accept)
			}

			// Business-scoped subtree — single chokepoint via RequireBusinessAccess.
			// Every route under /businesses/{id}/... is gated by this middleware, which:
			//   - parses and validates the business UUID from the URL
			//   - looks up membership (404 on non-member, not 403 — AUTHZ-05)
			//   - injects BusinessContext into ctx with the caller's role + permissions
			r.Route("/businesses/{id}", func(r chi.Router) {
				r.Use(authz.RequireBusinessAccess(authzCache, middleware.GetUserID))

				// Business profile.
				r.Get("/", handlers.Business.GetBusiness)
				r.Put("/", handlers.Business.UpdateBusiness)
				r.Put("/schedule", handlers.Business.UpdateSchedule)
				r.Put("/voice-tone", handlers.Business.UpdateVoiceTone)
				r.Put("/logo", handlers.Business.UploadLogo)
				r.Get("/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)

				// Integrations.
				// GET endpoints (list / auth-url / communities / locations) are
				// never gated by RequireVerifiedEmail* — read endpoints stay
				// open to unverified users per D-29.
				r.Get("/integrations", handlers.Integration.ListIntegrations)
				r.Delete("/integrations/{integrationId}", handlers.Integration.DeleteIntegration)

				// OAuth auth-url routes (need JWT + business context to generate state)
				r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
				r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
				r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
				r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)
				r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
				r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)

				// Phase 21-03 (ACCT-02 / D-26): day-0 hard-block on POST
				// /integrations/* — attacker surface for spam token connection.
				// Each connect endpoint is wrapped individually so the
				// decorator order stays explicit at the route declaration.
				integWith := func() func(http.Handler) http.Handler {
					if users == nil {
						return passThroughMiddleware
					}
					return middleware.RequireVerifiedEmailDay0(users)
				}()
				r.With(integWith).Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
				r.With(integWith).Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)
				r.With(integWith).Post("/integrations/yandex_business/probe", handlers.OAuth.ProbeYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/companies", handlers.OAuth.ListYandexCompanies)
				r.With(integWith).Post("/integrations/yandex_business/connect", handlers.OAuth.ConnectYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/{id}/refresh-name", handlers.OAuth.RefreshYandexBusinessName)
				r.With(integWith).Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)
				r.With(integWith).Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
				r.With(integWith).Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
				r.With(integWith).Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)

				// Chat (rate-limited via env-tunable RateLimits.Chat).
				// Phase 21-03 (ACCT-02 / D-28): day-7 soft-restrict gates
				// chat — unverified users have 7-day grace; reads / history
				// stay open. Layered AFTER RateLimitByUser so a throttled
				// request short-circuits before the DB lookup.
				if users != nil {
					r.With(
						middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute, "chat"),
						middleware.RequireVerifiedEmailDay7(users),
					).Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
				} else {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.Chat, time.Minute, "chat")).
						Post("/chat/{conversationID}", handlers.ChatProxy.Chat)
				}
				if handlers.HITL != nil {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.HITL, time.Minute, "chat")).
						Post("/chat/{id}/resume", handlers.HITL.Resume)
					r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
					r.Get("/tools", handlers.HITL.GetTools)
				}

				// Conversations.
				r.Get("/conversations", handlers.Conversation.ListConversations)
				r.Post("/conversations", handlers.Conversation.CreateConversation)
				r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
				r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
				r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
				r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
				r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)
				// Phase 19 (UI-03 / D-02): pin / unpin a conversation.
				r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
				r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
				// Phase 18 / TITLE-09: regenerate the auto-title for an existing chat.
				if handlers.Titler != nil {
					r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
				}

				// Projects (Phase 15).
				r.Get("/projects", handlers.Project.List)
				r.Post("/projects", handlers.Project.Create)
				r.Get("/projects/{id}", handlers.Project.Get)
				r.Put("/projects/{id}", handlers.Project.Update)
				r.Delete("/projects/{id}", handlers.Project.Delete)
				r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

				// Search.
				// Phase 19 / Plan 19-03 / SEARCH-02: sidebar search.
				if handlers.Search != nil {
					r.Get("/search", handlers.Search.Search)
				}

				// Review routes.
				r.Get("/reviews", handlers.Review.ListReviews)
				r.Get("/reviews/{id}", handlers.Review.GetReview)
				r.Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

				// Post routes.
				r.Get("/posts", handlers.Post.ListPosts)
				r.Get("/posts/{id}", handlers.Post.GetPost)

				// Agent task routes.
				r.Get("/tasks", handlers.AgentTask.ListTasks)
				r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)

				// Members + roles (NEW — Phase 2 v2.0 RBAC).
				if handlers.Members != nil {
					r.Get("/members", handlers.Members.ListMembers)
					r.Patch("/members/{userId}", handlers.Members.UpdateMemberRole)
					r.Delete("/members/{userId}", handlers.Members.RemoveMember)
				}
				if handlers.Roles != nil {
					r.Get("/roles", handlers.Roles.List)
					// Phase 5 RBAC role CRUD (ROLE-04..06). All gated by
					// RequireBusinessAccess (inherited from the subroute) +
					// per-route Can() check inside the handler.
					r.Post("/roles", handlers.Roles.Create)
					r.Patch("/roles/{roleId}", handlers.Roles.Update)
					r.Delete("/roles/{roleId}", handlers.Roles.Delete)
					// Phase 5 UI-RBAC-08 — actor's effective permissions in
					// the active business. No additional permission gate
					// beyond RequireBusinessAccess (any member can read their
					// own permissions).
					r.Get("/me/permissions", handlers.Roles.MyPermissions)
				}

				// Phase 3: invitations CRUD — business-scoped under
				// PermMembersInvite. Mirrors Members/Roles registration.
				// Phase 21-03 (ACCT-02 / D-26): POST is gated by day-0
				// soft-restrict — unverified users cannot send invites
				// (spam vector). GET and DELETE stay open.
				if handlers.Invitations != nil {
					if users != nil {
						r.With(middleware.RequireVerifiedEmailDay0(users)).
							Post("/invitations", handlers.Invitations.Create)
					} else {
						r.Post("/invitations", handlers.Invitations.Create)
					}
					r.Get("/invitations", handlers.Invitations.ListPending)
					r.Delete("/invitations/{inviteId}", handlers.Invitations.Revoke)
				}

				// Phase 19 Wave 5: audit log read endpoint. Gated by
				// PermAuditRead inside the handler (Owner+Admin via Phase 6
				// seed). RequireBusinessAccess (above) handles the membership
				// + business-id validation; the handler handles the
				// permission check + cursor/filter validation.
				if handlers.AuditLog != nil {
					r.Get("/audit-logs", handlers.AuditLog.List)
				}
			})
		})

		// Platform registry — public endpoint outside the auth group so the
		// marketing landing page can render without a token. Returns only
		// non-sensitive metadata (name, description, configured/availability).
		if handlers.Platforms != nil {
			r.Get("/platforms", handlers.Platforms.List)
		}
	})

	// Prometheus metrics
	r.Handle("/metrics", promhttp.Handler())

	// Health check
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers, hc *health.Checker) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(i18n.LocaleMiddleware)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)
	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}
