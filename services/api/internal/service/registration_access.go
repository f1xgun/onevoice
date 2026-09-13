package service

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// RegistrationAccessRepository is the closed-beta allow-list lookup seam.
type RegistrationAccessRepository interface {
	HasRegistrationAccess(ctx context.Context, email string) (bool, error)
}

// RegistrationAccessService keeps the registration authorization policy between
// the handler and persistence. It never treats an unavailable store as approval.
type RegistrationAccessService struct {
	repo RegistrationAccessRepository
}

func NewRegistrationAccessService(repo RegistrationAccessRepository) *RegistrationAccessService {
	return &RegistrationAccessService{repo: repo}
}

func (s *RegistrationAccessService) HasRegistrationAccess(ctx context.Context, email string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("registration access repository is not configured")
	}
	email = domain.NormalizeEmail(email)
	if email == "" {
		return false, nil
	}
	allowed, err := s.repo.HasRegistrationAccess(ctx, email)
	if err != nil {
		return false, fmt.Errorf("check registration access: %w", err)
	}
	return allowed, nil
}
