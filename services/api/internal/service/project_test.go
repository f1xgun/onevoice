package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// --- mock domain.ProjectRepository ----------------------------------------

// mockProjectRepository implements domain.ProjectRepository with overridable
// function fields so each test can inject only the behavior it needs. All
// methods of the interface (incl. HardDeleteCascade) are covered so the mock
// satisfies the contract at compile time.
type mockProjectRepository struct {
	createFunc                 func(ctx context.Context, p *domain.Project) error
	getByIDFunc                func(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	listByBusinessIDFunc       func(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error)
	updateFunc                 func(ctx context.Context, p *domain.Project) error
	deleteFunc                 func(ctx context.Context, id uuid.UUID) error
	countConversationsByIDFunc func(ctx context.Context, id uuid.UUID) (int, error)
	hardDeleteCascadeFunc      func(ctx context.Context, id uuid.UUID) (int, int, error)
}

func (m *mockProjectRepository) Create(ctx context.Context, p *domain.Project) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, p)
	}
	return nil
}
func (m *mockProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrProjectNotFound
}
func (m *mockProjectRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error) {
	if m.listByBusinessIDFunc != nil {
		return m.listByBusinessIDFunc(ctx, businessID)
	}
	return []domain.Project{}, nil
}
func (m *mockProjectRepository) Update(ctx context.Context, p *domain.Project) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, p)
	}
	return nil
}
func (m *mockProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}
func (m *mockProjectRepository) CountConversationsByID(ctx context.Context, id uuid.UUID) (int, error) {
	if m.countConversationsByIDFunc != nil {
		return m.countConversationsByIDFunc(ctx, id)
	}
	return 0, nil
}
func (m *mockProjectRepository) HardDeleteCascade(ctx context.Context, id uuid.UUID) (int, int, error) {
	if m.hardDeleteCascadeFunc != nil {
		return m.hardDeleteCascadeFunc(ctx, id)
	}
	return 0, 0, nil
}

// Compile-time check that our mock satisfies the interface. If the interface
// grows and we forget to update the mock, this line fails the build rather
// than silently passing a broken test.
var _ domain.ProjectRepository = (*mockProjectRepository)(nil)

// --- tests -----------------------------------------------------------------

func TestProjectService_Create(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()

	t.Run("error - empty name returns ErrProjectNameRequired", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{})
		_, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNameRequired)
	})

	t.Run("error - system_prompt too long returns ErrProjectSystemPromptTooLong", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{})
		_, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "X",
			SystemPrompt:  strings.Repeat("a", domain.MaxProjectSystemPromptChars+1),
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectSystemPromptTooLong)
	})

	t.Run("error - explicit mode with empty allowed_tools returns ErrProjectWhitelistEmpty", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{})
		_, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  nil,
		})
		assert.ErrorIs(t, err, domain.ErrProjectWhitelistEmpty)
	})

	t.Run("error - invalid whitelist_mode returns ErrProjectWhitelistMode", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{})
		_, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistMode("bogus"),
		})
		assert.ErrorIs(t, err, domain.ErrProjectWhitelistMode)
	})

	t.Run("success - happy path persists project and defaults nil slices to empty", func(t *testing.T) {
		var captured *domain.Project
		repo := &mockProjectRepository{
			createFunc: func(ctx context.Context, p *domain.Project) error {
				captured = p
				p.ID = uuid.New()
				return nil
			},
		}
		svc := NewProjectService(repo)

		got, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "Reviews",
			SystemPrompt:  "you reply",
			WhitelistMode: domain.WhitelistModeAll,
			// AllowedTools + QuickActions intentionally nil — service must
			// normalise to empty slices so JSON serialises as `[]` not `null`.
		})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, businessID, got.BusinessID)
		assert.Equal(t, "Reviews", got.Name)
		assert.Equal(t, domain.WhitelistModeAll, got.WhitelistMode)
		assert.NotNil(t, got.AllowedTools)
		assert.Len(t, got.AllowedTools, 0)
		assert.NotNil(t, got.QuickActions)
		assert.Len(t, got.QuickActions, 0)
		require.NotNil(t, captured)
		assert.Equal(t, businessID, captured.BusinessID)
	})

	t.Run("success - explicit with tools is accepted", func(t *testing.T) {
		repo := &mockProjectRepository{
			createFunc: func(ctx context.Context, p *domain.Project) error { return nil },
		}
		svc := NewProjectService(repo)
		got, err := svc.Create(ctx, businessID, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  []string{"telegram__send_channel_post"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"telegram__send_channel_post"}, got.AllowedTools)
	})
}

func TestProjectService_GetByID(t *testing.T) {
	ctx := context.Background()
	ownBusinessID := uuid.New()
	otherBusinessID := uuid.New()
	projectID := uuid.New()

	t.Run("error - ErrProjectNotFound bubbles", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return nil, domain.ErrProjectNotFound
			},
		}
		svc := NewProjectService(repo)
		_, err := svc.GetByID(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("cross-business access returns ErrProjectNotFound (no 403 leak)", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: projectID, BusinessID: otherBusinessID}, nil
			},
		}
		svc := NewProjectService(repo)
		_, err := svc.GetByID(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("success", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: projectID, BusinessID: ownBusinessID, Name: "X"}, nil
			},
		}
		svc := NewProjectService(repo)
		got, err := svc.GetByID(ctx, ownBusinessID, projectID)
		require.NoError(t, err)
		assert.Equal(t, "X", got.Name)
	})
}

func TestProjectService_Update(t *testing.T) {
	ctx := context.Background()
	ownBusinessID := uuid.New()
	otherBusinessID := uuid.New()
	projectID := uuid.New()

	t.Run("validation errors before loading the row", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				t.Fatal("GetByID should not be called on validation failure")
				return nil, nil
			},
		}
		svc := NewProjectService(repo)
		_, err := svc.Update(ctx, ownBusinessID, projectID, UpdateProjectInput{
			Name:          "",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNameRequired)
	})

	t.Run("cross-business returns ErrProjectNotFound", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: otherBusinessID}, nil
			},
			updateFunc: func(ctx context.Context, p *domain.Project) error {
				t.Fatal("Update should not be called on cross-business access")
				return nil
			},
		}
		svc := NewProjectService(repo)
		_, err := svc.Update(ctx, ownBusinessID, projectID, UpdateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("success - applies input and calls repo.Update", func(t *testing.T) {
		called := false
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: ownBusinessID, Name: "Old"}, nil
			},
			updateFunc: func(ctx context.Context, p *domain.Project) error {
				called = true
				assert.Equal(t, "New", p.Name)
				assert.Equal(t, domain.WhitelistModeExplicit, p.WhitelistMode)
				return nil
			},
		}
		svc := NewProjectService(repo)
		got, err := svc.Update(ctx, ownBusinessID, projectID, UpdateProjectInput{
			Name:          "New",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  []string{"vk__publish_post"},
		})
		require.NoError(t, err)
		assert.Equal(t, "New", got.Name)
		assert.True(t, called, "repo.Update should have been called")
	})
}

func TestProjectService_DeleteCascade(t *testing.T) {
	ctx := context.Background()
	ownBusinessID := uuid.New()
	otherBusinessID := uuid.New()
	projectID := uuid.New()

	t.Run("cross-business returns ErrProjectNotFound without cascading", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: otherBusinessID}, nil
			},
			hardDeleteCascadeFunc: func(ctx context.Context, id uuid.UUID) (int, int, error) {
				t.Fatal("cascade must not run on cross-business attempt")
				return 0, 0, nil
			},
		}
		svc := NewProjectService(repo)
		_, _, err := svc.DeleteCascade(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("success - returns deleted counts from repo.HardDeleteCascade", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: ownBusinessID}, nil
			},
			hardDeleteCascadeFunc: func(ctx context.Context, id uuid.UUID) (int, int, error) {
				return 3, 17, nil
			},
		}
		svc := NewProjectService(repo)
		convs, msgs, err := svc.DeleteCascade(ctx, ownBusinessID, projectID)
		require.NoError(t, err)
		assert.Equal(t, 3, convs)
		assert.Equal(t, 17, msgs)
	})
}

func TestProjectService_CountConversations(t *testing.T) {
	ctx := context.Background()
	ownBusinessID := uuid.New()
	otherBusinessID := uuid.New()
	projectID := uuid.New()

	t.Run("cross-business returns ErrProjectNotFound", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: otherBusinessID}, nil
			},
		}
		svc := NewProjectService(repo)
		_, err := svc.CountConversations(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("success", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: id, BusinessID: ownBusinessID}, nil
			},
			countConversationsByIDFunc: func(ctx context.Context, id uuid.UUID) (int, error) {
				return 42, nil
			},
		}
		svc := NewProjectService(repo)
		count, err := svc.CountConversations(ctx, ownBusinessID, projectID)
		require.NoError(t, err)
		assert.Equal(t, 42, count)
	})
}
