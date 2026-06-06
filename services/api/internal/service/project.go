package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// CreateProjectInput is the validated payload for Create / Update. The same
// shape is used for both operations (same form).
//
// ApprovalOverrides is the per-project tool-floor
// override map. Keys are tool names; values are "auto" or "manual".
// A key whose value is "inherit" at the request level MUST be stripped
// before reaching the repo — inherit is encoded as KEY ABSENCE in the
// persisted JSONB. The handler owns this
// transformation before passing the map down.
type CreateProjectInput struct {
	Name              string
	Description       string
	SystemPrompt      string
	WhitelistMode     domain.WhitelistMode
	AllowedTools      []string
	ApprovalOverrides map[string]domain.ToolFloor
	QuickActions      []string
}

// UpdateProjectInput is identical to CreateProjectInput (same form both
// operations). Alias so call sites read clearly.
type UpdateProjectInput = CreateProjectInput

// ProjectService wraps a single domain.ProjectRepository interface value
// (HardDeleteCascade is part of the interface). No type
// assertions, no anonymous widened interface — this is the wiring
// invariant.
type ProjectService struct {
	repo  domain.ProjectRepository
	audit audit.Logger
}

// NewProjectService constructs a ProjectService. The repo parameter is the
// single interface value that flows from cmd/main.go wiring.
//
// auditLogger is the second arg so Create/Update/
// DeleteCascade can emit project.* audit events AFTER the underlying repo
// write succeeds. nil-safe via audit.Nop so existing service tests don't
// have to thread a logger through every call site.
func NewProjectService(repo domain.ProjectRepository, auditLogger audit.Logger) *ProjectService {
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &ProjectService{repo: repo, audit: auditLogger}
}

// validate checks the inputs against the four domain invariants:
// - name required
// - system_prompt length cap (4000 chars, enforced in 3 places total)
// - whitelist_mode is one of the 4 known enum values
// - when mode=explicit, allowed_tools must not be empty (anti-footgun)
func (s *ProjectService) validate(input CreateProjectInput) error {
	if input.Name == "" {
		return domain.ErrProjectNameRequired
	}
	if len(input.SystemPrompt) > domain.MaxProjectSystemPromptChars {
		return domain.ErrProjectSystemPromptTooLong
	}
	if !domain.ValidWhitelistMode(input.WhitelistMode) {
		return domain.ErrProjectWhitelistMode
	}
	if input.WhitelistMode == domain.WhitelistModeExplicit && len(input.AllowedTools) == 0 {
		return domain.ErrProjectWhitelistEmpty
	}
	return nil
}

// Create validates the input and persists a new project for businessID.
//
// actorID identifies the user performing the
// create so the service can emit a project.created audit row AFTER the
// successful repo write.
func (s *ProjectService) Create(ctx context.Context, businessID, actorID uuid.UUID, input CreateProjectInput) (*domain.Project, error) {
	if err := s.validate(input); err != nil {
		return nil, err
	}
	p := &domain.Project{
		BusinessID:        businessID,
		Name:              input.Name,
		Description:       input.Description,
		SystemPrompt:      input.SystemPrompt,
		WhitelistMode:     input.WhitelistMode,
		AllowedTools:      nilToEmptyStrings(input.AllowedTools),
		ApprovalOverrides: input.ApprovalOverrides,
		QuickActions:      nilToEmptyStrings(input.QuickActions),
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	audit.LogProjectCreated(ctx, s.audit, businessID, actorID, p.ID, p.Name)

	return p, nil
}

// GetByID returns the project if it exists and is owned by businessID.
// Cross-business access returns ErrProjectNotFound — do NOT leak existence
// via a 403 (see docs/security.md).
func (s *ProjectService) GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.BusinessID != businessID {
		return nil, domain.ErrProjectNotFound
	}
	return p, nil
}

// ListByBusinessID returns all projects owned by the given business.
func (s *ProjectService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error) {
	return s.repo.ListByBusinessID(ctx, businessID)
}

// Update validates the input and applies edits if the project belongs to
// businessID. Cross-business attempts map to ErrProjectNotFound.
//
// actorID identifies the user performing the
// update so the service can emit a project.updated audit row AFTER the
// successful repo write.
func (s *ProjectService) Update(ctx context.Context, businessID, id, actorID uuid.UUID, input UpdateProjectInput) (*domain.Project, error) {
	if err := s.validate(input); err != nil {
		return nil, err
	}
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.BusinessID != businessID {
		return nil, domain.ErrProjectNotFound
	}
	p.Name = input.Name
	p.Description = input.Description
	p.SystemPrompt = input.SystemPrompt
	p.WhitelistMode = input.WhitelistMode
	p.AllowedTools = nilToEmptyStrings(input.AllowedTools)
	p.ApprovalOverrides = input.ApprovalOverrides
	p.QuickActions = nilToEmptyStrings(input.QuickActions)
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	audit.LogProjectUpdated(ctx, s.audit, businessID, actorID, id)

	return p, nil
}

// DeleteCascade hard-deletes the project plus every Mongo conversation/message
// assigned to it, returning the counts. Cross-business attempts map to
// ErrProjectNotFound.
//
// actorID is threaded so the service can emit a
// project.deleted audit row carrying blast-radius (deletedConversations).
func (s *ProjectService) DeleteCascade(ctx context.Context, businessID, id, actorID uuid.UUID) (deletedConversations, deletedMessages int, err error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return 0, 0, err
	}
	if p.BusinessID != businessID {
		return 0, 0, domain.ErrProjectNotFound
	}
	convs, msgs, err := s.repo.HardDeleteCascade(ctx, id)
	if err != nil {
		return convs, msgs, err
	}

	audit.LogProjectDeleted(ctx, s.audit, businessID, actorID, id, p.Name, convs)

	return convs, msgs, nil
}

// CountConversations returns how many Mongo conversations are currently
// assigned to the project.
func (s *ProjectService) CountConversations(ctx context.Context, businessID, id uuid.UUID) (int, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if p.BusinessID != businessID {
		return 0, domain.ErrProjectNotFound
	}
	return s.repo.CountConversationsByID(ctx, id)
}

// nilToEmptyStrings normalises nil slices so the JSON response always
// serializes as `[]` instead of `null`.
func nilToEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
