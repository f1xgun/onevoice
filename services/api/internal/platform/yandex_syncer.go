package platform

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// yandexRequestTimeout is generous: the RPA agent spins up a Chromium page,
// waits for the edit form to render, types into the hours input, and clicks
// save. Under normal load this completes in 20-40s; we allow up to 90s for
// retries inside the agent's withRetry wrapper.
const yandexRequestTimeout = 90 * time.Second

// errYandexPublisherUnconfigured is returned when SyncSchedule is called
// without a wired TaskPublisher. The dispatcher records this as the
// AgentTask error string verbatim — preserves prior log/UI assertions.
var errYandexPublisherUnconfigured = errors.New("NATS task publisher not configured")

// YandexSyncer dispatches schedule updates to the Yandex.Business RPA agent
// over NATS. Schedule is the only Yandex capability synced from the API
// today; profile-info edits go through a separate RPA tool path.
type YandexSyncer struct {
	taskPublisher TaskPublisher // optional; nil produces an "unconfigured" error
}

// Compile-time interface assertion.
var _ ScheduleSyncer = (*YandexSyncer)(nil)

// NewYandexSyncer wires a YandexSyncer. taskPublisher is optional: passing
// nil preserves the legacy behavior where Yandex sync silently fails with a
// recorded "publisher not configured" error task instead of panicking at
// startup (NATS may be intentionally absent in the API-only profile).
func NewYandexSyncer(taskPublisher TaskPublisher) *YandexSyncer {
	return &YandexSyncer{taskPublisher: taskPublisher}
}

// SyncSchedule dispatches a yandex_business__update_hours RPA task to the
// agent over NATS. Bypasses HITL — this is a user-initiated profile edit,
// the "Save schedule" click is the consent.
//
// Empty-schedule short-circuit lives in the dispatcher (so this method is
// only called when there is real work to do). The dispatcher also records
// the AgentTask; SyncSchedule only performs the RPC and returns errors.
func (y *YandexSyncer) SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error {
	hoursJSON := scheduleToYandexJSON(b.Settings)

	if y.taskPublisher == nil {
		slog.WarnContext(ctx, "platform sync: yandex_business: task publisher not configured")
		return errYandexPublisherUnconfigured
	}

	req := a2a.ToolRequest{
		TaskID:     uuid.New().String(),
		Tool:       tools.YandexBusinessUpdateHours,
		Args:       map[string]interface{}{"hours": hoursJSON},
		BusinessID: b.ID.String(),
	}

	resp, err := y.taskPublisher.RequestTool(ctx, a2a.Subject(a2a.AgentYandexBusiness), req, yandexRequestTimeout)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: yandex_business: request failed",
			"business_id", b.ID, "error", err)
		return err
	}
	if resp != nil && resp.Error != "" {
		slog.WarnContext(ctx, "platform sync: yandex_business: agent returned error",
			"business_id", b.ID, "error", resp.Error)
		return errors.New(resp.Error)
	}
	return nil
}
