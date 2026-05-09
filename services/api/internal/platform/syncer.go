// Package platform pushes business info updates to connected third-party
// platforms (Telegram, VK, Yandex.Business). The Syncer orchestrates dispatch
// across capability-segregated interfaces so each platform implements only the
// capabilities it supports — no no-op stub methods.
package platform

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Capability-segregated platform sync interfaces. Each platform implements
// only the capabilities it supports; SyncBusiness performs a type-assertion
// dispatch per capability so absence of an interface means "platform doesn't
// support this", with no no-op methods required.
type (
	// TitleSyncer pushes the business name to the platform's title field.
	TitleSyncer interface {
		SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error
	}

	// DescriptionSyncer pushes the business description (with formatting).
	DescriptionSyncer interface {
		SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error
	}

	// PhotoSyncer pushes the business logo to the platform's profile photo.
	PhotoSyncer interface {
		SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error
	}

	// ScheduleSyncer pushes opening-hours / schedule data.
	ScheduleSyncer interface {
		SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error
	}

	// InfoSyncer is for batched-update platforms (e.g. VK groups.edit) where
	// description + phone + website ship in a single API call. Do NOT split
	// a batched mutation into per-capability calls.
	InfoSyncer interface {
		SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error
	}
)

// integrationProvider fetches integration data for a business.
type integrationProvider interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (accessToken string, err error)
}

// taskRecorder creates AgentTask records for sync operations.
type taskRecorder interface {
	Create(ctx context.Context, task *domain.AgentTask) error
}

// TaskPublisher dispatches an A2A ToolRequest to a platform agent over NATS
// (or any equivalent transport in tests) and waits for the reply. Reply
// timeout is the responsibility of the implementation.
type TaskPublisher interface {
	RequestTool(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error)
}

// syncBusinessTimeout bounds the per-business fan-out across all platforms.
const syncBusinessTimeout = 30 * time.Second

// capabilityDispatch describes a per-capability dispatch entry: the AgentTask
// type, its Russian display name, an input-builder that may differ between
// the success and error branches (matching the verbatim shape of the prior
// switch-based dispatch), and the function that executes the call.
type capabilityDispatch struct {
	taskType    string
	displayName string
	input       func(err error) map[string]string
	fn          func(ctx context.Context, b *domain.Business, integ domain.Integration) error
}

// Syncer fans business updates out to every active platform integration via
// per-capability dispatch.
type Syncer struct {
	integrations integrationProvider
	tasks        taskRecorder
	hub          *taskhub.Hub   // optional; may be nil
	perPlatform  map[string]any // platform identifier → capability-implementing impl
}

// NewSyncer wires the Syncer with required collaborators. integrations and
// tasks are required (panic on nil to fail fast at startup); hub is optional.
// perPlatform may be empty (the dispatch loop simply finds no implementations
// and exits cleanly).
func NewSyncer(integrations integrationProvider, tasks taskRecorder, hub *taskhub.Hub, perPlatform map[string]any) *Syncer {
	if integrations == nil {
		panic("platform.NewSyncer: integrations cannot be nil")
	}
	if tasks == nil {
		panic("platform.NewSyncer: tasks cannot be nil")
	}
	if perPlatform == nil {
		perPlatform = map[string]any{}
	}
	return &Syncer{
		integrations: integrations,
		tasks:        tasks,
		hub:          hub,
		perPlatform:  perPlatform,
	}
}

// SyncBusiness pushes the updated business info to all active connected
// platforms. Designed to run in a goroutine (fire-and-forget); errors are
// only logged. Each capability runs independently — a failure in one does
// not skip the others.
func (s *Syncer) SyncBusiness(business *domain.Business) {
	ctx, cancel := context.WithTimeout(context.Background(), syncBusinessTimeout)
	defer cancel()

	integrations, err := s.integrations.ListByBusinessID(ctx, business.ID)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: failed to list integrations", "business_id", business.ID, "error", err)
		return
	}

	for _, integ := range integrations {
		if integ.Status != "active" {
			continue
		}
		platImpl, ok := s.perPlatform[integ.Platform]
		if !ok {
			continue
		}
		s.dispatchCapabilities(ctx, business, integ, platImpl)
	}
}

// dispatchCapabilities runs each capability the platform impl supports. The
// per-capability AgentTask records (start time, status, input fields) are
// preserved verbatim from the prior switch-based dispatch — same display
// names, same task types, same input shapes.
func (s *Syncer) dispatchCapabilities(ctx context.Context, b *domain.Business, integ domain.Integration, platImpl any) {
	if t, ok := platImpl.(TitleSyncer); ok {
		// Error branch records only channel_id; done branch additionally
		// records name.
		s.runWithTask(ctx, b, integ, capabilityDispatch{
			taskType:    "sync_title",
			displayName: "Синхронизация названия",
			input: func(err error) map[string]string {
				if err != nil {
					return map[string]string{"channel_id": integ.ExternalID}
				}
				return map[string]string{"channel_id": integ.ExternalID, "name": b.Name}
			},
			fn: t.SyncTitle,
		})
	}
	if d, ok := platImpl.(DescriptionSyncer); ok {
		s.runWithTask(ctx, b, integ, capabilityDispatch{
			taskType:    "sync_description",
			displayName: "Синхронизация описания",
			input:       func(error) map[string]string { return map[string]string{"channel_id": integ.ExternalID} },
			fn:          d.SyncDescription,
		})
	}
	if p, ok := platImpl.(PhotoSyncer); ok && b.LogoURL != "" {
		s.runWithTask(ctx, b, integ, capabilityDispatch{
			taskType:    "sync_photo",
			displayName: "Синхронизация фото",
			input:       func(error) map[string]string { return map[string]string{"channel_id": integ.ExternalID} },
			fn:          p.SyncPhoto,
		})
	}
	if i, ok := platImpl.(InfoSyncer); ok {
		input := vkInfoInput(b, integ)
		s.runWithTask(ctx, b, integ, capabilityDispatch{
			taskType:    "sync_info",
			displayName: "Синхронизация данных",
			input:       func(error) map[string]string { return input },
			fn:          i.SyncInfo,
		})
	}
	if sch, ok := platImpl.(ScheduleSyncer); ok {
		hours := scheduleToYandexJSON(b.Settings)
		// Schedule sync silent-skips when the business has no schedule
		// configured; preserve that "no noisy AgentTask row" behavior here
		// at the dispatch layer so SyncSchedule stays a thin verbatim move.
		// Skip is local to this branch so subsequently-added capabilities
		// after Schedule are unaffected.
		if hours != "" {
			input := map[string]string{"permalink": integ.ExternalID, "hours": hours}
			s.runWithTask(ctx, b, integ, capabilityDispatch{
				taskType:    "sync_hours",
				displayName: "Синхронизация часов работы",
				input:       func(error) map[string]string { return input },
				fn:          sch.SyncSchedule,
			})
		}
	}
}

// runWithTask wraps a capability invocation with start time, error capture,
// AgentTask creation, and taskhub publish. Lifted from the prior recordTask
// helper — preserves the existing AgentTask shape verbatim.
func (s *Syncer) runWithTask(ctx context.Context, b *domain.Business, integ domain.Integration, dispatch capabilityDispatch) {
	started := time.Now()
	err := dispatch.fn(ctx, b, integ)
	status := "done"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
	}
	s.recordTask(ctx, b.ID, integ.Platform, dispatch.taskType, dispatch.displayName, status, dispatch.input(err), errMsg, started)
}

// recordTask creates an AgentTask record (if a recorder is configured) for a
// sync operation that has already completed. startedAt is captured before the
// operation so the stored duration is meaningful. displayName is the human
// label shown on the Tasks page — callers pass the Russian string directly.
func (s *Syncer) recordTask(ctx context.Context, businessID uuid.UUID, platform, taskType, displayName, status string, input interface{}, errMsg string, startedAt time.Time) {
	if s.tasks == nil {
		return
	}
	completedAt := time.Now()
	task := &domain.AgentTask{
		BusinessID:  businessID.String(),
		Type:        taskType,
		DisplayName: displayName,
		Status:      status,
		Platform:    platform,
		Input:       input,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		CreatedAt:   completedAt,
		Error:       errMsg,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		slog.ErrorContext(ctx, "platform sync: failed to record task", "error", err)
		return
	}
	if s.hub != nil {
		s.hub.Publish(businessID.String(), taskhub.Event{Kind: taskhub.KindCreated, Task: *task})
	}
}

// vkInfoInput builds the AgentTask input map for VK groups.edit. Kept here
// (not inside vk_syncer.go) because the dispatcher must populate the input
// before invoking the capability — same shape the previous switch wrote to
// recordTask.
func vkInfoInput(b *domain.Business, integ domain.Integration) map[string]string {
	input := map[string]string{
		"group_id":    integ.ExternalID,
		"description": b.Description,
		"phone":       b.Phone,
	}
	if b.Website != nil {
		input["website"] = *b.Website
	}
	return input
}
