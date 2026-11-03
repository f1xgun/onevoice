package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

func (s *ReconciliationService) SetScheduleAudit(logger audit.Logger) { s.scheduleAudit = logger }

// AdoptYandexHours accepts the displayed snapshot locally. It never calls a
// platform writer and does not manufacture a new verification timestamp.
func (s *ReconciliationService) AdoptYandexHours(ctx context.Context, businessID, actorID uuid.UUID, externalID string, checkedAt time.Time) (*domain.Business, error) {
	adopter, ok := s.syncState.(domain.RemoteScheduleAdopter)
	if !ok {
		return nil, domain.ErrServiceUnavailable
	}
	if _, err := s.businessRepo.GetByID(ctx, businessID); err != nil {
		return nil, fmt.Errorf("load adopting business: %w", err)
	}
	integrations, err := s.integRepo.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("load adoption integration: %w", err)
	}
	active := false
	for _, integration := range integrations {
		if integration.Platform == a2a.AgentYandexBusiness && integration.ExternalID == externalID && integration.Status == domain.IntegrationStatusActive {
			active = true
		}
	}
	if !active {
		return nil, domain.ErrSyncStateChanged
	}
	states, err := s.syncState.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("load adoption comparison: %w", err)
	}
	for _, state := range states {
		if state.BusinessID != businessID || state.Platform != a2a.AgentYandexBusiness || state.ExternalID != externalID {
			continue
		}
		if state.LastCheckedAt == nil || checkedAt.IsZero() || !state.LastCheckedAt.Equal(checkedAt) || state.ConsecutiveFailures > 0 || state.LastError != "" {
			return nil, domain.ErrSyncStateChanged
		}
		schedule, err := platform.ScheduleFromYandexSnapshot(state.LastRemoteSnapshot[platform.FieldSchedule])
		if err != nil {
			return nil, domain.ErrSyncStateChanged
		}
		if err := adopter.AdoptSchedule(ctx, businessID, state, schedule); err != nil {
			return nil, fmt.Errorf("accept remote hours: %w", err)
		}
		updated, err := s.businessRepo.GetByID(ctx, businessID)
		if err != nil {
			return nil, fmt.Errorf("reload adopted hours: %w", err)
		}
		if s.scheduleAudit != nil {
			audit.LogBusinessUpdated(ctx, s.scheduleAudit, businessID, actorID)
		}
		return updated, nil
	}
	return nil, domain.ErrSyncStateChanged
}
