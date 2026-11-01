package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type specialDatesBrowser interface {
	UpdateSpecialDates(context.Context, domain.SpecialDateHoursChange) error
}

func (h *Handler) updateSpecialDates(ctx context.Context, req a2a.ToolRequest, raw interface{}) (*a2a.ToolResponse, error) {
	if _, exists := req.Args["hours"]; exists {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("hours and special_dates must be saved separately"))
	}
	value, ok := raw.(string)
	if !ok {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("special_dates must be a JSON string"))
	}
	change, err := domain.ParseSpecialDateHoursChange([]byte(value))
	if err != nil {
		return nil, a2a.NewNonRetryableError(err)
	}
	browser, err := h.getBrowser(ctx, req)
	if err != nil {
		return nil, err
	}
	dateBrowser, ok := browser.(specialDatesBrowser)
	if !ok {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("special-date editor unavailable"))
	}
	if err := dateBrowser.UpdateSpecialDates(ctx, change); err != nil {
		return nil, fmt.Errorf("yandex: update special dates: %w", classifyYandexError(err))
	}
	return a2a.OK(req, map[string]any{"status": "updated", "note": "changes pending Yandex moderation"}), nil
}
