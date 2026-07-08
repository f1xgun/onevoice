// Package router wires HTTP routes for the API service.
//
// See docs/api/routes.md for the route catalog, middleware stack, and
// security gating per route group.
package router

import (
	"log/slog"
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
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/handler/connect"
	"github.com/f1xgun/onevoice/services/api/internal/handler/oauth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// deletionGraceDaysForRouter mirrors service.AccountDeletionService.graceDays
// so BlockWritesDuringGrace can compute the 423 body's deletionDate.
const deletionGraceDaysForRouter = 30

// passThroughMiddleware is a no-op fallback used when a nil UserLookup is
// passed, keeping route declarations uniform.
func passThroughMiddleware(next http.Handler) http.Handler { return next }

// corsMaxAge is the CORS preflight cache lifetime in seconds.
const corsMaxAge = 300

// RateLimits aggregates the per-endpoint per-minute request budgets so
// router.Setup takes one parameter instead of four. See docs/api/routes.md
// for the budget rationale per field.
type RateLimits struct {
	Register    int
	Login       int
	Chat        int
	HITL        int
	Consents    int
	Telemetry   int
	Writes      int
	Invitations int
	Search      int
}

// Handlers encapsulates all HTTP handlers consumed by Setup / SetupInternal.
type Handlers struct {
	Auth                 *handler.AuthHandler
	Business             *handler.BusinessHandler
	Integration          *handler.IntegrationHandler
	Conversation         *handler.ConversationHandler
	OAuth                *oauth.OAuthHandler
	Connect              *connect.ConnectHandler
	InternalToken        *handler.InternalTokenHandler
	InternalBilling      *handler.InternalBillingHandler      // mTLS internal only
	InternalYandexShared *handler.InternalYandexSharedHandler // mTLS internal only
	ChatProxy            *handler.ChatProxyHandler
	Review               *handler.ReviewHandler
	PresenceHealth       *handler.PresenceHealthHandler
	Post                 *handler.PostHandler
	AgentTask            *handler.AgentTaskHandler
	Telemetry            *handler.TelemetryHandler
	Project              *handler.ProjectHandler
	HITL                 *handler.HITLHandler
	Titler               *handler.TitlerHandler
	Search               *handler.SearchHandler
	Platforms            *handler.PlatformsHandler
	Permissions          *handler.PermissionsHandler
	Members              *handler.MembersHandler
	Roles                *handler.RolesHandler
	Invitations          *handler.InvitationsHandler
	AuditLog             *handler.AuditLogHandler
	Billing              *handler.BillingHandler
	UserDeletion         *handler.UserDeletionHandler
	BusinessDeletion     *handler.BusinessDeletionHandler
	Consents             *handler.ConsentsHandler
	Feedback             *handler.FeedbackHandler
	ChannelRequest       *handler.ChannelRequestHandler
}

// Setup creates and configures the public Chi router. See
// docs/api/routes.md for the full route catalog, middleware stack, and
// nil-tolerance contract on UserLookup / pgPool / lock.
func Setup(handlers *Handlers, jwtSecret []byte, redisClient *redis.Client, hc *health.Checker, allowedOrigins []string, rateLimits RateLimits, authzCache *authz.Cache, users middleware.UserLookup, pgPool *pgxpool.Pool, lock *lockout.Lockout) *chi.Mux {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(middleware.RequestLogger(slog.Default()))
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Accept-Language", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Correlation-ID"},
		AllowCredentials: true,
		MaxAge:           corsMaxAge,
	}))
	r.Use(i18n.LocaleMiddleware)
	r.Use(middleware.SecurityHeaders())
	r.Use(metrics.HTTPMiddleware)

	r.Route("/api/v1", func(r chi.Router) {
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).Post("/auth/register", handlers.Auth.Register)
		if lock != nil {
			r.With(middleware.LockoutMiddleware(lock)).
				With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Post("/auth/login", handlers.Auth.Login)
		} else {
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/login", handlers.Auth.Login)
		}
		r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).Post("/auth/refresh", handlers.Auth.RefreshToken)

		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).
			Post("/auth/password-reset/request", handlers.Auth.RequestPasswordReset)
		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).
			Post("/auth/password-reset/confirm", handlers.Auth.ConfirmPasswordReset)

		r.With(middleware.RateLimit(redisClient, rateLimits.Register, time.Minute)).
			Post("/auth/verify-email/confirm", handlers.Auth.VerifyConfirm)

		r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
		r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
		r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)
		r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)

		r.Get("/platforms", handlers.Platforms.List)

		if handlers.Invitations != nil {
			r.With(middleware.RateLimit(redisClient, rateLimits.Login, time.Minute)).
				Get("/invitations/{token}", handlers.Invitations.Preview)
		}

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			r.Post("/auth/logout", handlers.Auth.Logout)
			r.Get("/auth/me", handlers.Auth.Me)
			r.Post("/auth/verify-email/resend", handlers.Auth.VerifyResend)
			r.Patch("/auth/email-before-verify", handlers.Auth.EmailBeforeVerify)
			if handlers.UserDeletion != nil {
				r.Delete("/users/me", handlers.UserDeletion.Delete)
				r.Post("/users/me/restore", handlers.UserDeletion.Restore)
			}
			if handlers.Consents != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/auth/consents", handlers.Consents.Reconsent)
				r.Get("/users/me/consents", handlers.Consents.ListMine)
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "consents")).
					Post("/users/me/consents/pdn/withdraw", handlers.Consents.WithdrawPDN)
			}
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			if pgPool != nil {
				r.Use(middleware.BlockWritesDuringGrace(pgPool, deletionGraceDaysForRouter))
			}

			r.Put("/auth/password", handlers.Auth.ChangePassword)
			r.Patch("/auth/locale", handlers.Auth.UpdatePreferredLocale)
			r.Patch("/auth/profile", handlers.Auth.UpdateProfile)

			if handlers.Permissions != nil {
				r.Get("/permissions", handlers.Permissions.List)
			}

			r.Get("/businesses", handlers.Business.ListUserBusinesses)
			if users != nil {
				r.With(middleware.RequireVerifiedEmailDay7(users)).
					Post("/businesses", handlers.Business.CreateBusiness)
			} else {
				r.Post("/businesses", handlers.Business.CreateBusiness)
			}

			r.With(middleware.RateLimitByUser(redisClient, rateLimits.Telemetry, time.Minute, "telemetry")).
				Post("/telemetry", handlers.Telemetry.Ingest)

			if handlers.Feedback != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Consents, time.Minute, "feedback")).
					Post("/feedback", handlers.Feedback.Submit)
			}

			if handlers.Invitations != nil {
				r.With(middleware.RateLimitByUser(redisClient, rateLimits.Login, time.Minute, "invite_accept")).
					Post("/invitations/{token}/accept", handlers.Invitations.Accept)
			}

			r.Route("/businesses/{id}", func(r chi.Router) {
				r.Use(authz.RequireBusinessAccess(authzCache, middleware.GetUserID))

				// Per-user rate limits for state-changing routes. writeLimit
				// guards routes that trigger external work (integration
				// connect/refresh, RPA probes, review reply/refresh, business
				// update, logo upload + schedule edits — both fan out to every
				// connected platform); inviteLimit is tighter for email-sending
				// invites.
				writeLimit := middleware.RateLimitByUser(redisClient, rateLimits.Writes, time.Minute, "writes")
				inviteLimit := middleware.RateLimitByUser(redisClient, rateLimits.Invitations, time.Minute, "invitations")

				r.Get("/", handlers.Business.GetBusiness)
				r.With(writeLimit).Put("/", handlers.Business.UpdateBusiness)
				if handlers.BusinessDeletion != nil {
					r.Delete("/", handlers.BusinessDeletion.Delete)
					r.Post("/restore", handlers.BusinessDeletion.Restore)
				}
				r.With(writeLimit).Put("/schedule", handlers.Business.UpdateSchedule)
				r.Put("/voice-tone", handlers.Business.UpdateVoiceTone)
				r.With(writeLimit).Put("/logo", handlers.Business.UploadLogo)
				r.Get("/tool-approvals", handlers.Business.GetBusinessToolApprovals)
				r.Put("/tool-approvals", handlers.Business.UpdateBusinessToolApprovals)
				r.Get("/description-template", handlers.Business.GetDescriptionTemplate)
				r.With(writeLimit).Put("/description-template", handlers.Business.UpdateDescriptionTemplate)
				r.Get("/voice-profile", handlers.Business.GetVoiceProfile)
				r.With(writeLimit).Put("/voice-profile", handlers.Business.UpdateVoiceProfile)
				r.Get("/review-autopilot", handlers.Business.GetReviewAutopilot)
				r.With(writeLimit).Put("/review-autopilot", handlers.Business.UpdateReviewAutopilot)
				r.Get("/owner-brief", handlers.Business.GetOwnerBrief)
				r.With(writeLimit).Put("/owner-brief", handlers.Business.UpdateOwnerBrief)

				r.Get("/integrations", handlers.Integration.ListIntegrations)
				r.Get("/integrations/drift", handlers.Integration.GetIntegrationsDrift)
				r.With(writeLimit).Delete("/integrations/{integrationId}", handlers.Integration.DeleteIntegration)

				r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
				r.Get("/integrations/vk/communities", handlers.OAuth.VKCommunities)
				r.Get("/integrations/vk/community-auth-url", handlers.OAuth.VKCommunityAuthURL)
				r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)
				r.Get("/integrations/yandex_business/delegated-config", handlers.OAuth.GetYandexDelegatedConfig)
				r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
				r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)

				integWith := func() func(http.Handler) http.Handler {
					base := passThroughMiddleware
					if users != nil {
						base = middleware.RequireVerifiedEmailDay0(users)
					}
					// Integration-dispatch routes (connect / refresh / probe /
					// companies) trigger external API calls, token storage, and
					// RPA browser launches, so they share the per-user write
					// budget on top of the verified-email gate.
					return func(next http.Handler) http.Handler { return writeLimit(base(next)) }
				}()
				r.With(integWith).Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
				r.With(integWith).Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)
				r.With(integWith).Post("/integrations/yandex_business/probe", handlers.OAuth.ProbeYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/companies", handlers.OAuth.ListYandexCompanies)
				r.With(integWith).Post("/integrations/yandex_business/connect", handlers.OAuth.ConnectYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/connect-delegated", handlers.OAuth.ConnectDelegatedYandexBusiness)
				r.With(integWith).Post("/integrations/yandex_business/verify-access", handlers.OAuth.VerifyYandexBusinessAccess)
				r.With(integWith).Post("/integrations/yandex_business/{id}/refresh-name", handlers.OAuth.RefreshYandexBusinessName)
				r.With(integWith).Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)
				r.With(integWith).Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
				r.With(integWith).Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
				r.With(integWith).Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)
				r.With(integWith).Post("/integrations/telegram/owner-link", handlers.Connect.StartTelegramOwnerLink)
				r.With(integWith).Post("/integrations/verify", handlers.Integration.VerifyIntegrations)

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
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.HITL, time.Minute, "hitl")).
						Post("/chat/{id}/resume", handlers.HITL.Resume)
					r.Post("/conversations/{id}/pending-tool-calls/{batch_id}/resolve", handlers.HITL.ResolvePendingToolCalls)
					r.Get("/tools", handlers.HITL.GetTools)
				}

				r.Get("/conversations", handlers.Conversation.ListConversations)
				r.Post("/conversations", handlers.Conversation.CreateConversation)
				r.Get("/conversations/{id}", handlers.Conversation.GetConversation)
				r.Put("/conversations/{id}", handlers.Conversation.UpdateConversation)
				r.Delete("/conversations/{id}", handlers.Conversation.DeleteConversation)
				r.Get("/conversations/{id}/messages", handlers.Conversation.ListMessages)
				r.Post("/conversations/{id}/move", handlers.Conversation.MoveConversation)
				r.Post("/conversations/{id}/pin", handlers.Conversation.Pin)
				r.Post("/conversations/{id}/unpin", handlers.Conversation.Unpin)
				if handlers.Titler != nil {
					r.Post("/conversations/{id}/regenerate-title", handlers.Titler.RegenerateTitle)
				}

				r.Get("/projects", handlers.Project.List)
				r.Post("/projects", handlers.Project.Create)
				r.Get("/projects/{id}", handlers.Project.Get)
				r.Put("/projects/{id}", handlers.Project.Update)
				r.Delete("/projects/{id}", handlers.Project.Delete)
				r.Get("/projects/{id}/conversation-count", handlers.Project.ConversationCount)

				if handlers.Search != nil {
					r.With(middleware.RateLimitByUser(redisClient, rateLimits.Search, time.Minute, "search")).
						Get("/search", handlers.Search.Search)
				}

				r.Get("/reviews", handlers.Review.ListReviews)
				r.With(writeLimit).Post("/reviews/refresh", handlers.Review.RefreshReviews)
				r.With(writeLimit).Post("/reviews/batch-draft", handlers.Review.BatchDraftReviews)
				r.With(writeLimit).Post("/reviews/bulk-approve", handlers.Review.BulkApproveReviews)
				r.Get("/reviews/sla", handlers.Review.GetReviewSLA)
				r.Get("/presence-health", handlers.PresenceHealth.GetPresenceHealth)
				r.Get("/reviews/{id}", handlers.Review.GetReview)
				r.With(writeLimit).Put("/reviews/{id}/reply", handlers.Review.ReplyToReview)

				r.Get("/posts", handlers.Post.ListPosts)
				r.Get("/posts/{id}", handlers.Post.GetPost)

				if handlers.ChannelRequest != nil {
					r.With(writeLimit).Post("/channel-requests", handlers.ChannelRequest.Create)
					r.Get("/channel-requests", handlers.ChannelRequest.List)
				}

				r.Get("/tasks", handlers.AgentTask.ListTasks)
				r.Get("/tasks/stream", handlers.AgentTask.StreamTasks)
				r.With(writeLimit).Post("/tasks/{taskId}/retry", handlers.AgentTask.RetryTask)

				if handlers.Members != nil {
					r.Get("/members", handlers.Members.ListMembers)
					r.Patch("/members/{userId}", handlers.Members.UpdateMemberRole)
					r.Delete("/members/{userId}", handlers.Members.RemoveMember)
				}
				if handlers.Roles != nil {
					r.Get("/roles", handlers.Roles.List)
					r.Post("/roles", handlers.Roles.Create)
					r.Patch("/roles/{roleId}", handlers.Roles.Update)
					r.Delete("/roles/{roleId}", handlers.Roles.Delete)
					r.Get("/me/permissions", handlers.Roles.MyPermissions)
				}

				if handlers.Invitations != nil {
					if users != nil {
						r.With(inviteLimit, middleware.RequireVerifiedEmailDay0(users)).
							Post("/invitations", handlers.Invitations.Create)
					} else {
						r.With(inviteLimit).Post("/invitations", handlers.Invitations.Create)
					}
					r.Get("/invitations", handlers.Invitations.ListPending)
					r.With(writeLimit).Delete("/invitations/{inviteId}", handlers.Invitations.Revoke)
				}

				if handlers.AuditLog != nil {
					r.Get("/audit-logs", handlers.AuditLog.List)
				}
				if handlers.Billing != nil {
					r.Get("/billing/summary", handlers.Billing.Summary)
				}
			})
		})

		if handlers.Platforms != nil {
			r.Get("/platforms", handlers.Platforms.List)
		}
	})

	// /metrics stays on this listener (PORT, default 8080) rather than the
	// mTLS internal listener because Prometheus scrapes api:8080/metrics over
	// the compose network without a client cert (see
	// observability/prometheus/prometheus.yml). It is NOT reachable from the
	// public internet: nginx proxies only /api/v1, /media, /health and / —
	// /metrics is intentionally excluded (see nginx/nginx.conf.template).
	// Moving it behind mTLS requires provisioning a Prometheus client cert
	// first; see docs/api/routes.md.
	r.Handle("/metrics", promhttp.Handler())

	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler())

	return r
}

// internalServiceIdentityAllowlist enumerates client cert CNs permitted to
// call identity-gated /internal/v1 routes. See docs/api/routes.md.
var internalServiceIdentityAllowlist = []string{"orchestrator", "api"}

// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers, hc *health.Checker, cfg *config.Config) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.CorrelationID())
	r.Use(i18n.LocaleMiddleware)
	r.Use(middleware.RequestLogger(slog.Default()))
	r.Use(middleware.Recoverer)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequirePlatformACL(cfg.InternalACL, nil))
		r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)
	})

	if handlers.InternalBilling != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireServiceIdentity(internalServiceIdentityAllowlist, nil))
			r.Post("/internal/v1/billing/usage_logs", handlers.InternalBilling.LogUsage)
			r.Get("/internal/v1/billing/daily_spend", handlers.InternalBilling.GetDailySpend)
		})
	}

	if handlers.InternalYandexShared != nil {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireServiceIdentity(internalServiceIdentityAllowlist, nil))
			r.Post("/internal/v1/yandex/shared-session", handlers.InternalYandexShared.SetSharedSession)
		})
	}

	r.Get("/health/live", hc.LiveHandler())
	r.Get("/health/ready", hc.ReadyHandler())
	r.Get("/health", hc.LiveHandler())

	return r
}
