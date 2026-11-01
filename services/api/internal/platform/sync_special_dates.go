package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type specialDatesSyncer interface {
	SyncSpecialDates(context.Context, *domain.Business, domain.Integration, domain.SpecialDateHoursChange) error
}

// SyncSpecialDatesContext runs only for an explicit special-date save. It does
// not dispatch profile fields or weekly hours and retains the owned-date
// change in the recorded task for reproducible retries.
func (s *Syncer) SyncSpecialDatesContext(ctx context.Context, b *domain.Business, change domain.SpecialDateHoursChange) {
	integrations, err := s.integrations.ListByBusinessID(ctx, b.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list integrations for special dates", "error", err)
		return
	}
	raw, err := json.Marshal(change)
	if err != nil {
		slog.ErrorContext(ctx, "encode special dates", "error", err)
		return
	}
	for _, integ := range integrations {
		if integ.Status != domain.IntegrationStatusActive {
			continue
		}
		impl, ok := s.perPlatform[integ.Platform].(specialDatesSyncer)
		if !ok {
			continue
		}
		s.runWithTask(ctx, b, integ, capabilityDispatch{
			taskType:       "sync_special_dates",
			displayName:    "Синхронизация особых дат",
			displayNameKey: "sync.hours",
			input: func(error) map[string]string {
				return map[string]string{"permalink": integ.ExternalID, "special_dates": string(raw)}
			},
			fn: func(ctx context.Context, b *domain.Business, integ domain.Integration) error {
				return impl.SyncSpecialDates(ctx, b, integ, change)
			},
		})
	}
}

func (y *YandexSyncer) SyncSpecialDates(ctx context.Context, b *domain.Business, integ domain.Integration, change domain.SpecialDateHoursChange) error {
	raw, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("encode special dates: %w", err)
	}
	return y.updateHours(ctx, b, map[string]interface{}{"special_dates": string(raw)})
}
