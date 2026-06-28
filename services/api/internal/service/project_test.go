package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
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
func (m *mockProjectRepository) HardDeleteCascade(ctx context.Context, id uuid.UUID) (deletedConversations, deletedMessages int, err error) {
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
		svc := NewProjectService(&mockProjectRepository{}, audit.Nop())
		_, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "",
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNameRequired)
	})

	t.Run("error - system_prompt too long returns ErrProjectSystemPromptTooLong", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{}, audit.Nop())
		_, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			SystemPrompt:  strings.Repeat("a", domain.MaxProjectSystemPromptChars+1),
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectSystemPromptTooLong)
	})

	t.Run("error - explicit mode with empty allowed_tools returns ErrProjectWhitelistEmpty", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{}, audit.Nop())
		_, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  nil,
		})
		assert.ErrorIs(t, err, domain.ErrProjectWhitelistEmpty)
	})

	t.Run("error - invalid whitelist_mode returns ErrProjectWhitelistMode", func(t *testing.T) {
		svc := NewProjectService(&mockProjectRepository{}, audit.Nop())
		_, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
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
		svc := NewProjectService(repo, audit.Nop())

		got, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "Reviews",
			SystemPrompt:  "you reply",
			WhitelistMode: domain.WhitelistModeAll,
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
		svc := NewProjectService(repo, audit.Nop())
		got, err := svc.Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  []string{tools.TelegramSendChannelPost},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{tools.TelegramSendChannelPost}, got.AllowedTools)
	})
}

// TestProjectService_Create_InputBounds asserts the storage- and LLM-cost
// bounds on the free-form project fields. Reverting the bounds in
// service.validate accepts the oversized inputs and these subtests flip to a
// nil error (fail-on-revert). A project sized exactly at the caps still
// passes, so the bounds reject only genuine overflow.
func TestProjectService_Create_InputBounds(t *testing.T) {
	ctx := context.Background()
	businessID := uuid.New()

	newSvc := func() *ProjectService {
		return NewProjectService(&mockProjectRepository{
			createFunc: func(ctx context.Context, p *domain.Project) error { return nil },
		}, audit.Nop())
	}

	t.Run("error - name over cap returns ErrProjectNameTooLong", func(t *testing.T) {
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          strings.Repeat("n", domain.MaxProjectNameChars+1),
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectNameTooLong)
	})

	t.Run("error - description over cap returns ErrProjectDescriptionTooLong", func(t *testing.T) {
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			Description:   strings.Repeat("d", domain.MaxProjectDescriptionChars+1),
			WhitelistMode: domain.WhitelistModeAll,
		})
		assert.ErrorIs(t, err, domain.ErrProjectDescriptionTooLong)
	})

	t.Run("error - too many allowed tools returns ErrProjectTooManyAllowedTools", func(t *testing.T) {
		over := make([]string, domain.MaxProjectAllowedTools+1)
		for i := range over {
			over[i] = "t"
		}
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeAll,
			AllowedTools:  over,
		})
		assert.ErrorIs(t, err, domain.ErrProjectTooManyAllowedTools)
	})

	t.Run("error - oversized allowed tool element returns ErrProjectAllowedToolTooLong", func(t *testing.T) {
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeAll,
			AllowedTools:  []string{strings.Repeat("t", domain.MaxProjectAllowedToolChars+1)},
		})
		assert.ErrorIs(t, err, domain.ErrProjectAllowedToolTooLong)
	})

	t.Run("error - too many quick actions returns ErrProjectTooManyQuickActions", func(t *testing.T) {
		over := make([]string, domain.MaxProjectQuickActions+1)
		for i := range over {
			over[i] = "q"
		}
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeAll,
			QuickActions:  over,
		})
		assert.ErrorIs(t, err, domain.ErrProjectTooManyQuickActions)
	})

	t.Run("error - oversized quick action element returns ErrProjectQuickActionTooLong", func(t *testing.T) {
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          "X",
			WhitelistMode: domain.WhitelistModeAll,
			QuickActions:  []string{strings.Repeat("q", domain.MaxProjectQuickActionChars+1)},
		})
		assert.ErrorIs(t, err, domain.ErrProjectQuickActionTooLong)
	})

	t.Run("success - inputs exactly at the caps are accepted", func(t *testing.T) {
		_, err := newSvc().Create(ctx, businessID, uuid.Nil, CreateProjectInput{
			Name:          strings.Repeat("n", domain.MaxProjectNameChars),
			Description:   strings.Repeat("d", domain.MaxProjectDescriptionChars),
			WhitelistMode: domain.WhitelistModeAll,
			AllowedTools:  []string{strings.Repeat("t", domain.MaxProjectAllowedToolChars)},
			QuickActions:  []string{strings.Repeat("q", domain.MaxProjectQuickActionChars)},
		})
		require.NoError(t, err)
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
		svc := NewProjectService(repo, audit.Nop())
		_, err := svc.GetByID(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("cross-business access returns ErrProjectNotFound (no 403 leak)", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: projectID, BusinessID: otherBusinessID}, nil
			},
		}
		svc := NewProjectService(repo, audit.Nop())
		_, err := svc.GetByID(ctx, ownBusinessID, projectID)
		assert.ErrorIs(t, err, domain.ErrProjectNotFound)
	})

	t.Run("success", func(t *testing.T) {
		repo := &mockProjectRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
				return &domain.Project{ID: projectID, BusinessID: ownBusinessID, Name: "X"}, nil
			},
		}
		svc := NewProjectService(repo, audit.Nop())
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
		svc := NewProjectService(repo, audit.Nop())
		_, err := svc.Update(ctx, ownBusinessID, projectID, uuid.Nil, UpdateProjectInput{
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
		svc := NewProjectService(repo, audit.Nop())
		_, err := svc.Update(ctx, ownBusinessID, projectID, uuid.Nil, UpdateProjectInput{
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
		svc := NewProjectService(repo, audit.Nop())
		got, err := svc.Update(ctx, ownBusinessID, projectID, uuid.Nil, UpdateProjectInput{
			Name:          "New",
			WhitelistMode: domain.WhitelistModeExplicit,
			AllowedTools:  []string{tools.VKPublishPost},
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
		svc := NewProjectService(repo, audit.Nop())
		_, _, err := svc.DeleteCascade(ctx, ownBusinessID, projectID, uuid.Nil)
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
		svc := NewProjectService(repo, audit.Nop())
		convs, msgs, err := svc.DeleteCascade(ctx, ownBusinessID, projectID, uuid.Nil)
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
		svc := NewProjectService(repo, audit.Nop())
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
		svc := NewProjectService(repo, audit.Nop())
		count, err := svc.CountConversations(ctx, ownBusinessID, projectID)
		require.NoError(t, err)
		assert.Equal(t, 42, count)
	})
}
