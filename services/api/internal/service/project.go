package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// CreateProjectInput is the validated payload for Create / Update. The same
// shape is used for both operations (per 15-CONTEXT D-02 — same form).
type CreateProjectInput struct {
	Name          string
	Description   string
	SystemPrompt  string
	WhitelistMode domain.WhitelistMode
	AllowedTools  []string
	QuickActions  []string
}

// UpdateProjectInput is identical to CreateProjectInput (same form both
// operations). Alias so call sites read clearly.
type UpdateProjectInput = CreateProjectInput

// ProjectService wraps a single domain.ProjectRepository interface value
// (HardDeleteCascade is part of the interface per Plan 15-01). No type
// assertions, no anonymous widened interface — this is the Plan 15-03 wiring
// invariant.
type ProjectService struct {
	repo domain.ProjectRepository
}

// NewProjectService constructs a ProjectService. The repo parameter is the
// single interface value that flows from cmd/main.go wiring.
func NewProjectService(repo domain.ProjectRepository) *ProjectService {
	return &ProjectService{repo: repo}
}

// validate checks the inputs against the four domain invariants:
//   - name required
//   - system_prompt length cap (4000 chars, enforced in 3 places total)
//   - whitelist_mode is one of the 4 known enum values
//   - when mode=explicit, allowed_tools must not be empty (D-17 anti-footgun)
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
func (s *ProjectService) Create(ctx context.Context, businessID uuid.UUID, input CreateProjectInput) (*domain.Project, error) {
	if err := s.validate(input); err != nil {
		return nil, err
	}
	p := &domain.Project{
		BusinessID:    businessID,
		Name:          input.Name,
		Description:   input.Description,
		SystemPrompt:  input.SystemPrompt,
		WhitelistMode: input.WhitelistMode,
		AllowedTools:  nilToEmptyStrings(input.AllowedTools),
		QuickActions:  nilToEmptyStrings(input.QuickActions),
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
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
func (s *ProjectService) Update(ctx context.Context, businessID, id uuid.UUID, input UpdateProjectInput) (*domain.Project, error) {
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
	p.QuickActions = nilToEmptyStrings(input.QuickActions)
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteCascade hard-deletes the project plus every Mongo conversation/message
// assigned to it, returning the counts. Cross-business attempts map to
// ErrProjectNotFound.
func (s *ProjectService) DeleteCascade(ctx context.Context, businessID, id uuid.UUID) (int, int, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return 0, 0, err
	}
	if p.BusinessID != businessID {
		return 0, 0, domain.ErrProjectNotFound
	}
	return s.repo.HardDeleteCascade(ctx, id)
}

// CountConversations returns how many Mongo conversations are currently
// assigned to the project. Used by the frontend delete-confirmation dialog
// (15-CONTEXT D-06).
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
