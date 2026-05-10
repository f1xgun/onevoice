package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// PostService defines the interface for post operations
type PostService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Post, error)
}

type postService struct {
	repo domain.PostRepository
}

// Compile-time check that postService implements PostService
var _ PostService = (*postService)(nil)

// NewPostService creates a new post service instance
func NewPostService(repo domain.PostRepository, _ BusinessService) PostService {
	return &postService{
		repo: repo,
	}
}

func (s *postService) List(ctx context.Context, businessID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error) {
	posts, total, err := s.repo.ListByBusinessID(ctx, businessID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list posts: %w", err)
	}

	return posts, total, nil
}

func (s *postService) GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Post, error) {
	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get post: %w", err)
	}

	if post.BusinessID != businessID.String() {
		return nil, domain.ErrPostNotFound
	}

	return post, nil
}
