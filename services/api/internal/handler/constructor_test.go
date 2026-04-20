package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// --- Minimal no-op mocks for interfaces required by constructors ---

type stubIntegrationService struct{}

func (s *stubIntegrationService) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationService) GetByBusinessAndPlatform(_ context.Context, _ uuid.UUID, _ string) (*domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationService) Delete(_ context.Context, _ uuid.UUID) error { return nil }

type stubConversationRepo struct{}

func (s *stubConversationRepo) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (s *stubConversationRepo) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	return nil, nil
}
func (s *stubConversationRepo) ListByUserID(_ context.Context, _ string, _, _ int) ([]domain.Conversation, error) {
	return nil, nil
}
func (s *stubConversationRepo) Update(_ context.Context, _ *domain.Conversation) error { return nil }
func (s *stubConversationRepo) Delete(_ context.Context, _ string) error               { return nil }
func (s *stubConversationRepo) UpdateProjectAssignment(_ context.Context, _ string, _ *string) error {
	return nil
}

// --- Tests ---

func TestNewAuthHandler_NilService_ReturnsError(t *testing.T) {
	h, err := NewAuthHandler(nil, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewBusinessHandler_NilService_ReturnsError(t *testing.T) {
	h, err := NewBusinessHandler(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewIntegrationHandler_NilIntegrationService_ReturnsError(t *testing.T) {
	h, err := NewIntegrationHandler(nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewIntegrationHandler_NilBusinessService_ReturnsError(t *testing.T) {
	h, err := NewIntegrationHandler(&stubIntegrationService{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewConversationHandler_NilConversationRepo_ReturnsError(t *testing.T) {
	h, err := NewConversationHandler(nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewConversationHandler_NilMessageRepo_ReturnsError(t *testing.T) {
	h, err := NewConversationHandler(&stubConversationRepo{}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewReviewHandler_NilService_ReturnsError(t *testing.T) {
	h, err := NewReviewHandler(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewPostHandler_NilService_ReturnsError(t *testing.T) {
	h, err := NewPostHandler(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewAgentTaskHandler_NilService_ReturnsError(t *testing.T) {
	h, err := NewAgentTaskHandler(nil, taskhub.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

func TestNewAgentTaskHandler_NilHub_ReturnsError(t *testing.T) {
	h, err := NewAgentTaskHandler(&mockAgentTaskService{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}
