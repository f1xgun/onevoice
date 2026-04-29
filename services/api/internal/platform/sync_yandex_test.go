package platform

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeIntegrations implements integrationProvider for sync tests.
type fakeIntegrations struct {
	list []domain.Integration
}

func (f *fakeIntegrations) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return f.list, nil
}

func (f *fakeIntegrations) GetDecryptedToken(_ context.Context, _ uuid.UUID, _, _ string) (string, error) {
	return "stub-token", nil
}

// fakeTaskRecorder captures AgentTask records for assertion.
type fakeTaskRecorder struct {
	mu    sync.Mutex
	tasks []*domain.AgentTask
}

func (f *fakeTaskRecorder) Create(_ context.Context, t *domain.AgentTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, t)
	return nil
}

func (f *fakeTaskRecorder) snapshot() []*domain.AgentTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.AgentTask, len(f.tasks))
	copy(out, f.tasks)
	return out
}

// fakeTaskPublisher captures RequestTool calls.
type fakeTaskPublisher struct {
	mu      sync.Mutex
	calls   []a2a.ToolRequest
	subject string
	resp    *a2a.ToolResponse
	err     error
}

func (f *fakeTaskPublisher) RequestTool(_ context.Context, subject string, req a2a.ToolRequest, _ time.Duration) (*a2a.ToolResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	f.subject = subject
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestScheduleToYandexJSON(t *testing.T) {
	t.Run("converts open and closed days into yandex shape", func(t *testing.T) {
		settings := map[string]interface{}{
			"schedule": []interface{}{
				map[string]interface{}{"day": "mon", "open": "09:00", "close": "21:00", "closed": false},
				map[string]interface{}{"day": "tue", "open": "09:00", "close": "21:00", "closed": false},
				map[string]interface{}{"day": "sun", "open": "", "close": "", "closed": true},
			},
		}
		raw := scheduleToYandexJSON(settings)
		require.NotEmpty(t, raw)

		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(raw), &parsed))
		mon, ok := parsed["monday"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "09:00", mon["open"])
		assert.Equal(t, "21:00", mon["close"])
		assert.Equal(t, "closed", parsed["sunday"])
	})

	t.Run("returns empty when schedule is missing", func(t *testing.T) {
		assert.Equal(t, "", scheduleToYandexJSON(nil))
		assert.Equal(t, "", scheduleToYandexJSON(map[string]interface{}{}))
	})

	t.Run("returns empty when all days are open with no times set", func(t *testing.T) {
		settings := map[string]interface{}{
			"schedule": []interface{}{
				map[string]interface{}{"day": "mon", "open": "", "close": "", "closed": false},
			},
		}
		assert.Equal(t, "", scheduleToYandexJSON(settings))
	})
}

func TestSyncBusiness_PublishesYandexHours(t *testing.T) {
	businessID := uuid.New()
	business := &domain.Business{
		ID:   businessID,
		Name: "Cafe",
		Settings: map[string]interface{}{
			"schedule": []interface{}{
				map[string]interface{}{"day": "mon", "open": "09:00", "close": "21:00", "closed": false},
				map[string]interface{}{"day": "sun", "open": "", "close": "", "closed": true},
			},
		},
	}

	integ := &fakeIntegrations{
		list: []domain.Integration{
			{Platform: "yandex_business", Status: "active", ExternalID: "permalink-123"},
		},
	}
	rec := &fakeTaskRecorder{}
	pub := &fakeTaskPublisher{resp: &a2a.ToolResponse{Success: true}}

	s := NewSyncer(integ, nil, "")
	s.SetTaskRecorder(rec)
	s.SetTaskPublisher(pub)

	s.SyncBusiness(business)

	require.Len(t, pub.calls, 1, "yandex_business integration must trigger one ToolRequest")
	got := pub.calls[0]
	assert.Equal(t, "tasks.yandex_business", pub.subject)
	assert.Equal(t, "yandex_business__update_hours", got.Tool)
	assert.Equal(t, businessID.String(), got.BusinessID)
	hoursArg, ok := got.Args["hours"].(string)
	require.True(t, ok, "args.hours must be a string")
	assert.Contains(t, hoursArg, "monday", "hours JSON must contain English day keys for the agent")
	assert.Contains(t, hoursArg, "sunday")

	tasks := rec.snapshot()
	require.Len(t, tasks, 1)
	assert.Equal(t, "yandex_business", tasks[0].Platform)
	assert.Equal(t, "sync_hours", tasks[0].Type)
	assert.Equal(t, "done", tasks[0].Status)
}

func TestSyncBusiness_NoYandexIntegration_NoPublish(t *testing.T) {
	pub := &fakeTaskPublisher{resp: &a2a.ToolResponse{Success: true}}
	s := NewSyncer(&fakeIntegrations{list: nil}, nil, "")
	s.SetTaskPublisher(pub)

	s.SyncBusiness(&domain.Business{ID: uuid.New()})
	assert.Empty(t, pub.calls)
}

func TestSyncBusiness_TaskPublisherNil_RecordsErrorTask(t *testing.T) {
	business := &domain.Business{
		ID: uuid.New(),
		Settings: map[string]interface{}{
			"schedule": []interface{}{
				map[string]interface{}{"day": "mon", "open": "09:00", "close": "21:00", "closed": false},
			},
		},
	}

	integ := &fakeIntegrations{
		list: []domain.Integration{
			{Platform: "yandex_business", Status: "active", ExternalID: "permalink-1"},
		},
	}
	rec := &fakeTaskRecorder{}

	s := NewSyncer(integ, nil, "")
	s.SetTaskRecorder(rec)
	// no SetTaskPublisher → nil

	s.SyncBusiness(business)

	tasks := rec.snapshot()
	require.Len(t, tasks, 1)
	assert.Equal(t, "error", tasks[0].Status)
	assert.Contains(t, tasks[0].Error, "NATS task publisher not configured")
}

func TestSyncBusiness_AgentReturnsError_RecordsErrorTask(t *testing.T) {
	business := &domain.Business{
		ID: uuid.New(),
		Settings: map[string]interface{}{
			"schedule": []interface{}{
				map[string]interface{}{"day": "mon", "open": "09:00", "close": "21:00", "closed": false},
			},
		},
	}

	integ := &fakeIntegrations{
		list: []domain.Integration{
			{Platform: "yandex_business", Status: "active", ExternalID: "permalink-1"},
		},
	}
	rec := &fakeTaskRecorder{}
	pub := &fakeTaskPublisher{err: errors.New("nats timeout")}

	s := NewSyncer(integ, nil, "")
	s.SetTaskRecorder(rec)
	s.SetTaskPublisher(pub)

	s.SyncBusiness(business)

	tasks := rec.snapshot()
	require.Len(t, tasks, 1)
	assert.Equal(t, "error", tasks[0].Status)
	assert.Contains(t, tasks[0].Error, "nats timeout")
}
