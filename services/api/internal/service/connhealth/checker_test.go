package connhealth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type fakeProbe struct {
	tg Result
	vk Result
}

func (f *fakeProbe) CheckTelegramHealth(_ context.Context, _ string) Result { return f.tg }
func (f *fakeProbe) CheckVKHealth(_ context.Context, _ uuid.UUID, _ string) Result {
	return f.vk
}

type fakeDispatcher struct {
	resp *a2a.ToolResponse
	err  error
}

func (f *fakeDispatcher) RequestTool(_ context.Context, _ string, _ a2a.ToolRequest, _ time.Duration) (*a2a.ToolResponse, error) {
	return f.resp, f.err
}

type fakeStore struct {
	list    []domain.Integration
	updated map[uuid.UUID]map[string]interface{}
	listErr error
}

func newFakeStore(list ...domain.Integration) *fakeStore {
	return &fakeStore{list: list, updated: map[uuid.UUID]map[string]interface{}{}}
}

func (f *fakeStore) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return f.list, f.listErr
}

// SetMetadataKeys mirrors the repository's targeted jsonb_set: it merges the
// given top-level keys into the row's CURRENT metadata (a prior write if any,
// else the seeded list row), replacing each supplied key wholesale and
// preserving every sibling key.
func (f *fakeStore) SetMetadataKeys(_ context.Context, id uuid.UUID, keys map[string]interface{}) error {
	merged := map[string]interface{}{}
	if cur, ok := f.updated[id]; ok {
		for k, v := range cur {
			merged[k] = v
		}
	} else {
		for i := range f.list {
			if f.list[i].ID == id {
				for k, v := range f.list[i].Metadata {
					merged[k] = v
				}
			}
		}
	}
	for k, v := range keys {
		merged[k] = v
	}
	f.updated[id] = merged
	return nil
}

func TestCheckAll_PersistsAndReturnsPerIntegrationHealth(t *testing.T) {
	bizID := uuid.New()
	tgID, vkID := uuid.New(), uuid.New()
	store := newFakeStore(
		domain.Integration{ID: tgID, BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "-100"},
		domain.Integration{ID: vkID, BusinessID: bizID, Platform: a2a.AgentVK, ExternalID: "42"},
	)
	probe := &fakeProbe{
		tg: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()},
		vk: Result{Status: StatusBroken, ReasonCode: ReasonVKWallScopeMissing, CheckedAt: time.Now()},
	}
	c := NewChecker(probe, nil, store, nil)

	got, err := c.CheckAll(context.Background(), bizID)
	if err != nil {
		t.Fatalf("CheckAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 health rows, got %d", len(got))
	}
	if _, ok := store.updated[tgID]; !ok {
		t.Fatalf("expected UpdateMetadata called for telegram integration")
	}
	if _, ok := store.updated[vkID]; !ok {
		t.Fatalf("expected UpdateMetadata called for vk integration")
	}
	byPlatform := map[string]IntegrationHealth{}
	for _, h := range got {
		byPlatform[h.Platform] = h
	}
	if byPlatform[a2a.AgentTelegram].Status != StatusActive {
		t.Fatalf("expected telegram active, got %q", byPlatform[a2a.AgentTelegram].Status)
	}
	if byPlatform[a2a.AgentVK].Status != StatusBroken || byPlatform[a2a.AgentVK].ReasonCode != ReasonVKWallScopeMissing {
		t.Fatalf("expected vk broken/wall_scope_missing, got %+v", byPlatform[a2a.AgentVK])
	}
}

func TestCheckIntegration_FailSoftKeepsPriorActive(t *testing.T) {
	bizID := uuid.New()
	id := uuid.New()
	prior := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	store := newFakeStore()
	probe := &fakeProbe{tg: Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: time.Now()}}
	c := NewChecker(probe, nil, store, nil)

	res, _, err := c.CheckIntegration(context.Background(), domain.Integration{
		ID: id, BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "-100", Metadata: prior,
	})
	if err != nil {
		t.Fatalf("CheckIntegration: %v", err)
	}
	if res.Status != StatusActive {
		t.Fatalf("expected fail-soft to keep prior active, got %q", res.Status)
	}
	persisted := ReadFromMetadata(store.updated[id])
	if persisted.Status != StatusActive {
		t.Fatalf("expected persisted status active, got %q", persisted.Status)
	}
}

func TestProbeYandex_SessionExpiredMapsBroken(t *testing.T) {
	c := NewChecker(nil, &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}, newFakeStore(), nil)
	got := c.probeYandex(context.Background(), domain.Integration{BusinessID: uuid.New()}, time.Now())
	if got.Status != StatusBroken || got.ReasonCode != ReasonYandexSessionExpiry {
		t.Fatalf("expected broken/yandex_session_expired, got %+v", got)
	}
}

func TestProbeYandex_TimeoutMapsUnknown(t *testing.T) {
	c := NewChecker(nil, &fakeDispatcher{err: errors.New("nats request: context deadline exceeded")}, newFakeStore(), nil)
	got := c.probeYandex(context.Background(), domain.Integration{BusinessID: uuid.New()}, time.Now())
	if got.Status != StatusUnknown {
		t.Fatalf("expected fail-soft unknown on NATS timeout, got %q", got.Status)
	}
}

func TestProbeYandex_SuccessMapsActive(t *testing.T) {
	c := NewChecker(nil, &fakeDispatcher{resp: &a2a.ToolResponse{Success: true}}, newFakeStore(), nil)
	got := c.probeYandex(context.Background(), domain.Integration{BusinessID: uuid.New()}, time.Now())
	if got.Status != StatusActive {
		t.Fatalf("expected active on get_info success, got %q", got.Status)
	}
}

func TestProbeYandex_NilDispatcherUnknown(t *testing.T) {
	c := NewChecker(nil, nil, newFakeStore(), nil)
	got := c.probeYandex(context.Background(), domain.Integration{BusinessID: uuid.New()}, time.Now())
	if got.Status != StatusUnknown {
		t.Fatalf("expected unknown when dispatcher unwired, got %q", got.Status)
	}
}
