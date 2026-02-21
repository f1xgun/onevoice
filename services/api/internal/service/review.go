package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// ReviewService defines the interface for review operations
type ReviewService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Review, error)
	Reply(ctx context.Context, userID uuid.UUID, id string, replyText string) error
}

type reviewService struct {
	repo            domain.ReviewRepository
	businessService BusinessService
}

// Compile-time check that reviewService implements ReviewService
var _ ReviewService = (*reviewService)(nil)

// NewReviewService creates a new review service instance
func NewReviewService(repo domain.ReviewRepository, businessService BusinessService) ReviewService {
	return &reviewService{
		repo:            repo,
		businessService: businessService,
	}
}

func (s *reviewService) List(ctx context.Context, userID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("get business: %w", err)
	}

	reviews, total, err := s.repo.ListByBusinessID(ctx, business.ID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}

	return reviews, total, nil
}

func (s *reviewService) GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Review, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get business: %w", err)
	}

	review, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get review: %w", err)
	}

	if review.BusinessID != business.ID.String() {
		return nil, domain.ErrReviewNotFound
	}

	return review, nil
}

func (s *reviewService) Reply(ctx context.Context, userID uuid.UUID, id string, replyText string) error {
	if replyText == "" {
		return fmt.Errorf("reply text cannot be empty")
	}

	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get business: %w", err)
	}

	review, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}

	if review.BusinessID != business.ID.String() {
		return domain.ErrReviewNotFound
	}

	if err := s.repo.UpdateReply(ctx, id, replyText, "replied"); err != nil {
		return fmt.Errorf("update reply: %w", err)
	}

	return nil
}
