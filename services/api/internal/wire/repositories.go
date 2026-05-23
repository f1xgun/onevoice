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

	// MembershipLoader backs the authz cache (Phase 2 v2.0 RBAC). Same
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
		User:               repository.NewUserRepository(h.PG),
		Business:           repository.NewBusinessRepository(h.PG),
		BusinessMembership: repository.NewBusinessMembershipRepository(h.PG),
		Role:               repository.NewRoleRepository(h.PG),
		Integration:        repository.NewIntegrationRepository(h.PG),
		Conversation:       repository.NewConversationRepository(h.Mongo),
		Message:            repository.NewMessageRepository(h.Mongo),
		Review:             repository.NewReviewRepository(h.Mongo),
		Post:               repository.NewPostRepository(h.Mongo),
		AgentTask:          repository.NewAgentTaskRepository(h.Mongo),
		Project:            repository.NewProjectRepository(h.PG, h.Mongo),
		Invitation:         repository.NewInvitationRepository(h.PG),
		AuditLog:           repository.NewAuditLogRepository(h.PG),
		MembershipLoader:   repository.NewMembershipLoader(h.PG),
	}
}
