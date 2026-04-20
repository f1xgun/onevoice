package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Mock BusinessRepository
type mockBusinessRepository struct {
	createFunc              func(ctx context.Context, business *domain.Business) error
	getByIDFunc             func(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	getByUserIDFunc         func(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
	updateFunc              func(ctx context.Context, business *domain.Business) error
	updateToolApprovalsFunc func(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

func (m *mockBusinessRepository) Create(ctx context.Context, business *domain.Business) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, business)
	}
	return nil
}

func (m *mockBusinessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrBusinessNotFound
}

func (m *mockBusinessRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	if m.getByUserIDFunc != nil {
		return m.getByUserIDFunc(ctx, userID)
	}
	return nil, domain.ErrBusinessNotFound
}

func (m *mockBusinessRepository) Update(ctx context.Context, business *domain.Business) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, business)
	}
	return nil
}

func (m *mockBusinessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	if m.updateToolApprovalsFunc != nil {
		return m.updateToolApprovalsFunc(ctx, businessID, approvals)
	}
	return nil
}

func TestBusinessService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		var createdBusiness *domain.Business

		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				createdBusiness = business
				// Simulate repository setting timestamps
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			UserID:      userID,
			Name:        "Test Coffee Shop",
			Category:    "cafe",
			Address:     "123 Main St",
			Phone:       "+1234567890",
			Description: "Best coffee in town",
			LogoURL:     "https://example.com/logo.png",
			Settings:    map[string]interface{}{"theme": "dark"},
		}

		result, err := svc.Create(ctx, business)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, userID, result.UserID)
		assert.Equal(t, "Test Coffee Shop", result.Name)
		assert.Equal(t, "cafe", result.Category)
		assert.Equal(t, "123 Main St", result.Address)
		assert.Equal(t, "+1234567890", result.Phone)
		assert.Equal(t, "Best coffee in town", result.Description)
		assert.Equal(t, "https://example.com/logo.png", result.LogoURL)
		assert.NotNil(t, result.Settings)
		assert.Equal(t, "dark", result.Settings["theme"])
		assert.NotZero(t, result.CreatedAt)
		assert.NotZero(t, result.UpdatedAt)

		// Verify repository was called
		assert.NotNil(t, createdBusiness)
		assert.Equal(t, userID, createdBusiness.UserID)
	})

	t.Run("success with minimal fields", func(t *testing.T) {
		userID := uuid.New()
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			UserID: userID,
			Name:   "Minimal Business",
		}

		result, err := svc.Create(ctx, business)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, userID, result.UserID)
		assert.Equal(t, "Minimal Business", result.Name)
		assert.Empty(t, result.Category)
		assert.Empty(t, result.Address)
		assert.Empty(t, result.Phone)
	})

	t.Run("success with nil settings", func(t *testing.T) {
		userID := uuid.New()
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			UserID:   userID,
			Name:     "Business with nil settings",
			Settings: nil,
		}

		result, err := svc.Create(ctx, business)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Nil(t, result.Settings)
	})

	t.Run("error - empty name", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo)

		business := &domain.Business{
			UserID: uuid.New(),
			Name:   "",
		}

		result, err := svc.Create(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("error - nil user id", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo)

		business := &domain.Business{
			UserID: uuid.Nil,
			Name:   "Test Business",
		}

		result, err := svc.Create(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "user id is required")
	})

	t.Run("error - business already exists", func(t *testing.T) {
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				return domain.ErrBusinessExists
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			UserID: uuid.New(),
			Name:   "Test Business",
		}

		result, err := svc.Create(ctx, business)

		assert.ErrorIs(t, err, domain.ErrBusinessExists)
		assert.Nil(t, result)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database connection failed")
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				return repoErr
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			UserID: uuid.New(),
			Name:   "Test Business",
		}

		result, err := svc.Create(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "create business")
	})
}

func TestBusinessService_GetByUserID(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		existingBusiness := &domain.Business{
			ID:          uuid.New(),
			UserID:      userID,
			Name:        "Test Coffee Shop",
			Category:    "cafe",
			Address:     "123 Main St",
			Phone:       "+1234567890",
			Description: "Best coffee in town",
			LogoURL:     "https://example.com/logo.png",
			Settings:    map[string]interface{}{"theme": "dark"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		repo := &mockBusinessRepository{
			getByUserIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				if id == userID {
					return existingBusiness, nil
				}
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByUserID(ctx, userID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, existingBusiness.ID, result.ID)
		assert.Equal(t, existingBusiness.UserID, result.UserID)
		assert.Equal(t, existingBusiness.Name, result.Name)
	})

	t.Run("business not found", func(t *testing.T) {
		repo := &mockBusinessRepository{
			getByUserIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByUserID(ctx, uuid.New())

		assert.ErrorIs(t, err, domain.ErrBusinessNotFound)
		assert.Nil(t, result)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockBusinessRepository{
			getByUserIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, repoErr
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByUserID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get business by user id")
	})
}

func TestBusinessService_GetByID(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		businessID := uuid.New()
		existingBusiness := &domain.Business{
			ID:          businessID,
			UserID:      uuid.New(),
			Name:        "Test Coffee Shop",
			Category:    "cafe",
			Address:     "123 Main St",
			Phone:       "+1234567890",
			Description: "Best coffee in town",
			LogoURL:     "https://example.com/logo.png",
			Settings:    map[string]interface{}{"theme": "dark"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				if id == businessID {
					return existingBusiness, nil
				}
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByID(ctx, businessID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, existingBusiness.ID, result.ID)
		assert.Equal(t, existingBusiness.UserID, result.UserID)
		assert.Equal(t, existingBusiness.Name, result.Name)
	})

	t.Run("business not found", func(t *testing.T) {
		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByID(ctx, uuid.New())

		assert.ErrorIs(t, err, domain.ErrBusinessNotFound)
		assert.Nil(t, result)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, repoErr
			},
		}

		svc := NewBusinessService(repo)
		result, err := svc.GetByID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get business")
	})
}

func TestBusinessService_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		businessID := uuid.New()
		userID := uuid.New()
		var updatedBusiness *domain.Business

		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				updatedBusiness = business
				// Simulate repository updating timestamp
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			ID:          businessID,
			UserID:      userID,
			Name:        "Updated Coffee Shop",
			Category:    "restaurant",
			Address:     "456 New St",
			Phone:       "+9876543210",
			Description: "Updated description",
			LogoURL:     "https://example.com/new-logo.png",
			Settings:    map[string]interface{}{"theme": "light"},
		}

		result, err := svc.Update(ctx, business)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, businessID, result.ID)
		assert.Equal(t, "Updated Coffee Shop", result.Name)
		assert.Equal(t, "restaurant", result.Category)
		assert.Equal(t, "456 New St", result.Address)
		assert.Equal(t, "+9876543210", result.Phone)
		assert.Equal(t, "Updated description", result.Description)
		assert.Equal(t, "https://example.com/new-logo.png", result.LogoURL)
		assert.NotNil(t, result.Settings)
		assert.Equal(t, "light", result.Settings["theme"])
		assert.NotZero(t, result.UpdatedAt)

		// Verify repository was called
		assert.NotNil(t, updatedBusiness)
		assert.Equal(t, businessID, updatedBusiness.ID)
	})

	t.Run("success - clearing optional fields", func(t *testing.T) {
		businessID := uuid.New()
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			ID:          businessID,
			UserID:      uuid.New(),
			Name:        "Business Name",
			Category:    "",
			Address:     "",
			Phone:       "",
			Description: "",
			LogoURL:     "",
			Settings:    nil,
		}

		result, err := svc.Update(ctx, business)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Category)
		assert.Empty(t, result.Address)
		assert.Empty(t, result.Phone)
		assert.Empty(t, result.Description)
		assert.Empty(t, result.LogoURL)
		assert.Nil(t, result.Settings)
	})

	t.Run("error - empty name", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo)

		business := &domain.Business{
			ID:     uuid.New(),
			UserID: uuid.New(),
			Name:   "",
		}

		result, err := svc.Update(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo)

		business := &domain.Business{
			ID:     uuid.Nil,
			UserID: uuid.New(),
			Name:   "Test Business",
		}

		result, err := svc.Update(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - business not found", func(t *testing.T) {
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				return domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			ID:     uuid.New(),
			UserID: uuid.New(),
			Name:   "Test Business",
		}

		result, err := svc.Update(ctx, business)

		assert.ErrorIs(t, err, domain.ErrBusinessNotFound)
		assert.Nil(t, result)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				return repoErr
			},
		}

		svc := NewBusinessService(repo)
		business := &domain.Business{
			ID:     uuid.New(),
			UserID: uuid.New(),
			Name:   "Test Business",
		}

		result, err := svc.Update(ctx, business)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update business")
	})
}
