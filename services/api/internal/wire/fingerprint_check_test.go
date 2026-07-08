package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// stubIntegrationRepo is a minimal domain.IntegrationRepository that returns
// configurable values for CountIntegrationsWithDifferentFingerprint.
type stubIntegrationRepo struct {
	count int
	err   error
}

func (s *stubIntegrationRepo) CountIntegrationsWithDifferentFingerprint(_ context.Context, _ string) (int, error) {
	return s.count, s.err
}

func (s *stubIntegrationRepo) Create(_ context.Context, _ *domain.Integration) error { return nil }
func (s *stubIntegrationRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (s *stubIntegrationRepo) GetByBusinessAndPlatform(_ context.Context, _ uuid.UUID, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (s *stubIntegrationRepo) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationRepo) ListByBusinessAndPlatform(_ context.Context, _ uuid.UUID, _ string) ([]domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationRepo) GetByBusinessPlatformExternal(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (s *stubIntegrationRepo) GetActiveByPlatformExternal(_ context.Context, _, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (s *stubIntegrationRepo) Update(_ context.Context, _ *domain.Integration) error { return nil }
func (s *stubIntegrationRepo) Delete(_ context.Context, _ uuid.UUID) error           { return nil }
func (s *stubIntegrationRepo) SoftDelete(_ context.Context, _ uuid.UUID) error       { return nil }
func (s *stubIntegrationRepo) DeleteOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (s *stubIntegrationRepo) MarkTokenExpired(_ context.Context, _ uuid.UUID, _, _ string) (int64, error) {
	return 0, nil
}
func (s *stubIntegrationRepo) UpdateMetadata(_ context.Context, _ uuid.UUID, _ map[string]interface{}) error {
	return nil
}
func (s *stubIntegrationRepo) SetMetadataKeys(_ context.Context, _ uuid.UUID, _ map[string]interface{}) error {
	return nil
}
func (s *stubIntegrationRepo) UpdateExternalID(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *stubIntegrationRepo) ListAllActiveByPlatforms(_ context.Context, _ []string) ([]domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationRepo) SelectForRekey(_ context.Context, _ pgx.Tx, _ int16, _ int) ([]domain.Integration, error) {
	return nil, nil
}
func (s *stubIntegrationRepo) UpdateEnvelopeFieldsTx(_ context.Context, _ pgx.Tx, _ domain.Integration) error {
	return nil
}
func (s *stubIntegrationRepo) CountRekeyRemaining(_ context.Context, _ int16) (int, error) {
	return 0, nil
}

func TestFingerprintCheck_NoMismatch_NoFatal(t *testing.T) {
	repo := &stubIntegrationRepo{count: 0, err: nil}
	err := RunFingerprintCheck(context.Background(), repo, "aabbccdd")
	require.NoError(t, err)
}

func TestFingerprintCheck_RepoError_PropagatesError(t *testing.T) {
	repo := &stubIntegrationRepo{count: 0, err: errors.New("db down")}
	err := RunFingerprintCheck(context.Background(), repo, "aabbccdd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fingerprint check")
	assert.Contains(t, err.Error(), "db down")
}
