package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// PostService defines the interface for post operations
type PostService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Post, error)
}

type postService struct {
	repo            domain.PostRepository
	businessService BusinessService
}

// Compile-time check that postService implements PostService
var _ PostService = (*postService)(nil)

// NewPostService creates a new post service instance
func NewPostService(repo domain.PostRepository, businessService BusinessService) PostService {
	return &postService{
		repo:            repo,
		businessService: businessService,
	}
}

func (s *postService) List(ctx context.Context, userID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("get business: %w", err)
	}

	posts, total, err := s.repo.ListByBusinessID(ctx, business.ID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list posts: %w", err)
	}

	return posts, total, nil
}

func (s *postService) GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Post, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get business: %w", err)
	}

	post, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get post: %w", err)
	}

	if post.BusinessID != business.ID.String() {
		return nil, domain.ErrPostNotFound
	}

	return post, nil
}
