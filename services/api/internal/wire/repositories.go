package wire

import (
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// Repos aggregates every domain repository the API service consumes.
//
// Fields use the domain interface types (not concrete repository pointers)
// so handlers and services stay decoupled from the postgres/mongo split.
type Repos struct {
	User               domain.UserRepository
	Business           domain.BusinessRepository
	BusinessMembership domain.BusinessMembershipRepository
	Role               domain.RoleRepository
	Integration        domain.IntegrationRepository
	Conversation       domain.ConversationRepository
	Message            domain.MessageRepository
	Review             domain.ReviewRepository
	Post               domain.PostRepository
	AgentTask          domain.AgentTaskRepository
	Project            domain.ProjectRepository
	Invitation         domain.InvitationRepository
	AuditLog           domain.AuditLogRepository

	// email_outbox repository. Concrete pointer (not a
	// domain interface) because the worker and downstream
	// services depend on the methods directly — there is no need for
	// the indirection.
	EmailOutbox *repository.EmailOutboxRepository

	// password_reset_tokens repository. Concrete
	// pointer for the same reason as EmailOutbox — only the
	// PasswordResetService depends on it and the methods are used
	// directly without an intervening interface.
	PasswordResetToken *repository.PasswordResetTokenRepository

	// tx-aware password setter, exposed separately from the
	// domain.UserRepository interface (which stays tx-free). The concrete
	// *repository.UserResetExtAdapter satisfies service.UserRepoForReset
	// AND service.VerifyUserRepo by structural typing — wire/services.go
	// passes this value directly into NewPasswordResetService and
	// NewEmailVerificationService.
	UserResetExt *repository.UserResetExtAdapter

	// email_verification_tokens repository.
	EmailVerificationToken *repository.EmailVerificationTokenRepository

	// user_consents repository — stub for
	// to extend.
	UserConsents *repository.UserConsentsRepository

	// MembershipLoader backs the authz cache (v2.0 RBAC). Same
	// query surface as BusinessMembership but exposed as the typed
	// authz.MembershipLoader interface to keep the cache decoupled.
	MembershipLoader authz.MembershipLoader
}

// Repositories constructs every domain repository against the connections
// in h. Pure factory: no side effects, no logging, no errors. Each
// constructor either returns a value (Postgres-backed repos) or returns
// without failure modes (Mongo-backed repos).
func Repositories(h *DBHandles) *Repos {
	return &Repos{
		User:                   repository.NewUserRepository(h.PG),
		Business:               repository.NewBusinessRepository(h.PG),
		BusinessMembership:     repository.NewBusinessMembershipRepository(h.PG),
		Role:                   repository.NewRoleRepository(h.PG),
		Integration:            repository.NewIntegrationRepository(h.PG),
		Conversation:           repository.NewConversationRepository(h.Mongo),
		Message:                repository.NewMessageRepository(h.Mongo),
		Review:                 repository.NewReviewRepository(h.Mongo),
		Post:                   repository.NewPostRepository(h.Mongo),
		AgentTask:              repository.NewAgentTaskRepository(h.Mongo),
		Project:                repository.NewProjectRepository(h.PG, h.Mongo),
		Invitation:             repository.NewInvitationRepository(h.PG),
		AuditLog:               repository.NewAuditLogRepository(h.PG),
		EmailOutbox:            repository.NewEmailOutboxRepository(h.PG),
		PasswordResetToken:     repository.NewPasswordResetTokenRepository(h.PG),
		UserResetExt:           repository.NewUserResetExtAdapter(h.PG),
		EmailVerificationToken: repository.NewEmailVerificationTokenRepository(h.PG),
		UserConsents:           repository.NewUserConsentsRepository(h.PG),
		MembershipLoader:       repository.NewMembershipLoader(h.PG),
	}
}
