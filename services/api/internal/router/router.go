package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// grace-period constant. Mirrors
// service.AccountDeletionService.graceDays — the BlockWritesDuringGrace
// middleware needs the value to compute the 423 body's deletionDate.
const deletionGraceDaysForRouter = 30

// passThroughMiddleware is a no-op middleware used by the soft-restrict
// wrappers when the caller passes a nil UserLookup (legacy / test deploys
// that don't wire yet). Preserves existing behavior in those
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
	Consents int // per-minute budget for /auth/consents + /users/me/consents/pdn/withdraw
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
	// POST /internal/v1/billing/usage_logs — internal-only billing write path
	// consumed by the orchestrator (via pkg/billingclient). Lives under the
	// same mTLS-protected SetupInternal mux as InternalToken.
	InternalBilling *handler.InternalBillingHandler
	ChatProxy       *handler.ChatProxyHandler
	Review          *handler.ReviewHandler
	Post            *handler.PostHandler
	AgentTask       *handler.AgentTaskHandler
	Telemetry       *handler.TelemetryHandler
	Project         *handler.ProjectHandler
	HITL            *handler.HITLHandler         // resolve + resume + GET /tools
	Titler          *handler.TitlerHandler       // POST /conversations/{id}/regenerate-title
	Search          *handler.SearchHandler       // GET /api/v1/search
	Platforms       *handler.PlatformsHandler    // Public platform registry
	Permissions     *handler.PermissionsHandler  // RBAC: static permission registry
	Members         *handler.MembersHandler      // RBAC: member management
	Roles           *handler.RolesHandler        // RBAC: role listing
	Invitations     *handler.InvitationsHandler  // RBAC: invitation lifecycle
	AuditLog        *handler.AuditLogHandler     // audit-log read endpoint
	UserDeletion    *handler.UserDeletionHandler // DELETE /users/me + restore
	Consents        *handler.ConsentsHandler     // re-consent + withdraw + list
}

// Setup creates and configures the Chi router with all routes and middleware.
// allowedOrigins is the CORS whitelist sourced from CORS_ALLOWED_ORIGINS;
// callers MUST supply at least one entry so the public frontend origin can be
// configured per-environment without rebuilding the binary.
// rateLimits controls per-endpoint per-minute request budgets — see the
// RateLimits comment block above for the role of each field.
// authzCache backs the RequireBusinessAccess middleware that gates the
// /businesses/{id}/.. subtree (v2.0 RBAC).
// users is the UserLookup the soft-restrict middleware reads
// email_verified from on every protected request.
// May be nil — when nil, the soft-restrict decorators degrade to
// pass-through (legacy/test compat).
// pgPool is the shared pool the BlockWritesDuringGrace
// middleware reads users.deletion_requested_at from on every write
// request. May be nil — when nil, the grace gate degrades to pass-
// through (legacy/test compat).
// lock is the lockout state that gates /auth/login. May be nil — when nil,
// LockoutMiddleware is skipped and Login degrades to legacy behavior (no
// brute-force throttle beyond chi-level rate limit).
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits, authzCache *authz.Cache, users middleware.UserLookup, pgPool *pgxpool.Pool, lock *lockout.Lockout) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	// chi's RealIP middleware was intentionally removed: it rewrites
	// r.RemoteAddr from X-Forwarded-For unconditionally with no trust-set
	// knob, which lets an attacker spoof the upstream peer IP and bypass
	// the TRUSTED_PROXY_CIDRS trust gate in middleware.ClientIP.
	// middleware.ClientIP is now the single source of truth for "did this
	// XFF entry come from a trusted proxy?".
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
	// off the Accept-Language header. See pkg/i18n.
	r.Use(i18n.LocaleMiddleware)
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		// LockoutMiddleware mounted on /auth/login ONLY.
		// NOT on /auth/register (rate-limited at the outbox layer).
		// NOT on /auth/password-reset (self-unlock path — adding lockout
		// there would defeat the whole point). NOT on /auth/refresh
		// (cookie-bearer auth, brute-force has nothing to brute against).
		// Order matters: lockout BEFORE rate-limit so a locked account
		// returns 423 (not 429) — the 423 carries retry_after_seconds the
		// frontend needs for the lockout UI.
		if lock != nil {
			r.With(middleware.LockoutMiddleware(lock)).
				With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Post("/auth/login", handlers.Auth.Login)
		} else {
			// Graceful disable: no Redis at boot → legacy login path with rate-limit only.
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		}
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		// Password reset.
		// No middleware.RateLimit chi wrapper: the handler does its own
		// per-email Redis rate-limit inside the service so the
		// timing-parity contract is enforced uniformly.
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

		// Email verification confirm.
		// PUBLIC: no JWT required. The verify-email page in the FE is
		// reachable when the user is logged out (e.g. clicks the link in
		// another browser). Returns 204 with NO Set-Cookie / session
		// material — T-VE-02 mitigation. NO GET handler — the FE renders
		// a button-gated page off the ?token=… query string and POSTs
		// only after the user clicks (scanner-protection ).
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

		// public invitation preview — token IS the auth.
		// Rate-limited per-IP with the Login budget — same threat model as login
		// (automated abuse with no auth state). mitigation.
		if handlers.Invitations != nil {
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Get("/invitations/{token}", handlers.Invitations.Preview)
		}

		// always-reachable
		// authenticated routes. These are NEVER decorated by
		// BlockWritesDuringGrace because they are the user's escape
		// hatches from the soft-deleted state (restore + delete +
		// verify) OR they're idempotent reads (me + logout).
		// Verify endpoints from 21-03 also live here (right to
		// erasure / right to verify cannot be gated by other
		// middleware).
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)
			// verify resend + email-before-verify.
			r.Post("/auth/verify-email/resend", handlers.Auth.VerifyResend)
			r.Patch("/auth/email-before-verify", handlers.Auth.EmailBeforeVerify)
			// DELETE /users/me + POST /users/me/restore
			// Always reachable: DELETE is idempotent (second
			// call surfaces 423 from the service layer, not the
			// middleware); Restore is the explicit escape hatch.
			if handlers.UserDeletion != nil {
				r.Delete("/users/me", handlers.UserDeletion.Delete)
				r.Post("/users/me/restore", handlers.UserDeletion.Restore)
			}
			// re-consent + withdraw + list. Always reachable
			// (precedent — right-to-erasure / right-to-withdraw
			// cannot be gated by verification or grace per 152-ФЗ Art. 21).
			// mitigation: per-user rate limit on the two write
			// endpoints (Redis-backed, scope="consents"). GET stays
			// unthrottled — listing your own consents is non-mutating and
			// already userID-scoped. Withdrawal triggers the 30-day
			// deletion flow , so abuse is naturally self-limiting,
			// but the budget guards against accidental client retry loops.
			if handlers.Consents != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/auth/consents", handlers.Consents.Reconsent)
				r.Get("/users/me/consents", handlers.Consents.ListMine)
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/users/me/consents/pdn/withdraw", handlers.Consents.WithdrawPDN)
			}
		})

		// Protected routes (require auth + write-gated by
		// grace middleware when pgPool is provided).
		r.Group(func(r chi.Router) {
			// Auth middleware
			r.Use(middleware.Auth(jwtSecret))
			// block POST/PUT/PATCH/DELETE for users
			// inside the 30-day grace window. GETs bypass at the
			// middleware layer (method-check guard).
			if pgPool != nil {
				r.Use(middleware.BlockWritesDuringGrace(pgPool, deletionGraceDaysForRouter))
			}

			r.Put("/auth/password", handlers.Auth.ChangePassword)
			// i18n: persist the user's UI language choice
			// ('ru'|'en'). Sits next to /auth/me/password as a sibling
			// account-self-service endpoint; the frontend syncs the cookie ↔
			// DB on login via GET /auth/me reading preferred_locale + this
			// PATCH writing it. PATCH (not PUT) because the request is a
			// partial — only the locale field — and PATCH is the verb the
			// rest of the API uses for scalar mutations (e.g.
			// /members/{userId}).
			r.Patch("/auth/locale", handlers.Auth.UpdatePreferredLocale)

			// v2.0 RBAC: static permission registry.
			// Auth-required (any logged-in user) — no business scope.
			if handlers.Permissions != nil {
				r.Get("/permissions", handlers.Permissions.List)
			}

			// BIZ-02 + BIZ-03 (auth-only, NOT business-scoped).
			r.Get("/businesses", handlers.Business.ListUserBusinesses)
			// POST /businesses is gated by the
			// day-7 soft-restrict — unverified users get 7 days to convert
			// before business creation is blocked. Listing remains open
			// (read endpoints are banner-only ).
			if users != nil {
				r.With(middleware.RequireVerifiedEmailDay7(users)).
					Post("/businesses", handlers.Business.CreateBusiness)
			} else {
				r.Post("/businesses", handlers.Business.CreateBusiness)
			}

			// manual review refresh — kicks the cross-business sync;
			// no business scope so any authed user can trigger it.
			r.Post("/reviews/refresh", handlers.Review.RefreshReviews)

			r.Post("/telemetry", handlers.Telemetry.Ingest)

			// invitation accept — auth-required, NOT business-scoped
			// (the {token} URL param targets a specific business inside the
			// invitation row). Rate-limited per-user with Login budget for
			// defense-in-depth (RESEARCH OQ-06). / mitigation.
			if handlers.Invitations != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Login, time.Minute, "invite_accept")).
					Post("/invitations/{token}/accept", handlers.Invitations.Accept)
			}

			// Business-scoped subtree — single chokepoint via RequireBusinessAccess.
			// Every route under /businesses/{id}/... is gated by this middleware, which:
			// - parses and validates the business UUID from the URL
			// - looks up membership (404 on non-member, not 403)
			// - injects BusinessContext into ctx with the caller's role + permissions
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
				// open to unverified users.
				r.Get("/integrations", handlers.Integration.ListIntegrations)
				r.Delete("/integrations/{integrationId}", handlers.Integration.DeleteIntegration)

				// OAuth auth-url routes (need JWT + business context to generate state)
				r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
				r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
				r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
				r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)
				r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
				r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)

				// day-0 hard-block on POST
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
				// day-7 soft-restrict gates
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
				// (UI-03): pin / unpin a conversation.
				r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
				r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
				// TITLE-09: regenerate the auto-title for an existing chat.
				if handlers.Titler != nil {
					r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
				}

				// Projects.
				r.Get("/projects", handlers.Project.List)
				r.Post("/projects", handlers.Project.Create)
				r.Get("/projects/{id}", handlers.Project.Get)
				r.Put("/projects/{id}", handlers.Project.Update)
				r.Delete("/projects/{id}", handlers.Project.Delete)
				r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

				// Search.
				// sidebar search.
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

				// Members + roles (NEW — v2.0 RBAC).
				if handlers.Members != nil {
					r.Get("/members", handlers.Members.ListMembers)
					r.Patch("/members/{userId}", handlers.Members.UpdateMemberRole)
					r.Delete("/members/{userId}", handlers.Members.RemoveMember)
				}
				if handlers.Roles != nil {
					r.Get("/roles", handlers.Roles.List)
					// RBAC role CRUD (ROLE-04.06). All gated by
					// RequireBusinessAccess (inherited from the subroute) +
					// per-route Can check inside the handler.
					r.Post("/roles", handlers.Roles.Create)
					r.Patch("/roles/{roleId}", handlers.Roles.Update)
					r.Delete("/roles/{roleId}", handlers.Roles.Delete)
					// UI-RBAC-08 — actor's effective permissions in
					// the active business. No additional permission gate
					// beyond RequireBusinessAccess (any member can read their
					// own permissions).
					r.Get("/me/permissions", handlers.Roles.MyPermissions)
				}

				// invitations CRUD — business-scoped under
				// PermMembersInvite. Mirrors Members/Roles registration.
				// POST is gated by day-0
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

				// audit log read endpoint. Gated by
				// PermAuditRead inside the handler (Owner+Admin via
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

// internalServiceIdentityAllowlist enumerates the client cert CNs permitted
// to call any identity-gated /internal/v1 route. Kept narrow on purpose:
//   - orchestrator: the primary caller (LLM router → billingclient).
//   - api: defense-in-depth slot for the future case where the api service
//     dials its own internal listener (e.g. titler self-bills). Today the
//     api wires Repos.Billing directly in-process for those callers, so the
//     entry is currently unused but reserved.
//
// Platform agents (telegram / vk / yandex_business / google_business) are
// intentionally NOT allowlisted — they do not bill in v1.4.
var internalServiceIdentityAllowlist = []string{"orchestrator", "api"}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers, hc *health.Checker) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(i18n.LocaleMiddleware)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)

	// Billing endpoints. Gated by RequireServiceIdentity so only the
	// orchestrator (or api self-call) can append usage rows or read spend.
	// Defense in depth on top of the listener-level mTLS handshake.
	if handlers.InternalBilling != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireServiceIdentity(internalServiceIdentityAllowlist, nil))
			r.Post("/internal/v1/billing/usage_logs", handlers.InternalBilling.LogUsage)
			// Daily-spend read path consumed by the orchestrator's rate-limiter
			// before every chat turn. Same mTLS + service-identity gate as the
			// write path so it cannot be probed off-cluster.
			r.Get("/internal/v1/billing/daily_spend", handlers.InternalBilling.GetDailySpend)
		})
	}

	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler()) // backward compat

	return r
}
