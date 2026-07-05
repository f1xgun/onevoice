package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// fakeOwnerLinkBinder records Bind calls so the consumer test can assert what
// the plane forwarded and simulate every bad-token outcome.
type fakeOwnerLinkBinder struct {
	mu      sync.Mutex
	enabled bool
	bindErr error
	bizID   uuid.UUID
	calls   []ownerLinkBindCall
}

type ownerLinkBindCall struct {
	token    string
	fromID   int64
	username string
}

func (f *fakeOwnerLinkBinder) Enabled() bool { return f.enabled }

func (f *fakeOwnerLinkBinder) Bind(_ context.Context, token string, fromID int64, username string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ownerLinkBindCall{token: token, fromID: fromID, username: username})
	if f.bindErr != nil {
		return uuid.Nil, f.bindErr
	}
	return f.bizID, nil
}

func (f *fakeOwnerLinkBinder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeOwnerLinkBinder) lastCall() ownerLinkBindCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// --- handle: valid handshake forwards token + authentic from.id -------------

func TestOwnerLinkConsumer_ValidHandshake_ForwardsTokenAndFromID(t *testing.T) {
	binder := &fakeOwnerLinkBinder{enabled: true, bizID: uuid.New()}
	consumer := service.NewTelegramOwnerLinkConsumer(binder)

	const bigID = int64(9007199254740993) // > 2^53
	if err := consumer.HandleForTest(context.Background(), a2a.TelegramOwnerLink{
		Token:    "opaque-token-value",
		FromID:   bigID,
		Username: "owner_handle",
	}); err != nil {
		t.Fatalf("handle error: %v", err)
	}

	if binder.callCount() != 1 {
		t.Fatalf("expected exactly one bind call, got %d", binder.callCount())
	}
	got := binder.lastCall()
	if got.token != "opaque-token-value" || got.fromID != bigID || got.username != "owner_handle" {
		t.Fatalf("bind called with wrong args: %+v", got)
	}
}

// --- handle: bad-token paths are safe no-ops (no error surfaced, no leak) ----

func TestOwnerLinkConsumer_BadToken_NoOpNoError(t *testing.T) {
	for _, bindErr := range []error{domain.ErrLinkTokenInvalid, domain.ErrIntegrationNotFound} {
		t.Run(bindErr.Error(), func(t *testing.T) {
			binder := &fakeOwnerLinkBinder{enabled: true, bizID: uuid.New(), bindErr: bindErr}
			consumer := service.NewTelegramOwnerLinkConsumer(binder)

			if err := consumer.HandleForTest(context.Background(), a2a.TelegramOwnerLink{
				Token:  "whatever",
				FromID: 123,
			}); err != nil {
				t.Fatalf("bad-token path must be a safe no-op (nil error), got %v", err)
			}
		})
	}
}

// --- handle: an infrastructure error is surfaced (worth logging) ------------

func TestOwnerLinkConsumer_InfraError_Surfaced(t *testing.T) {
	binder := &fakeOwnerLinkBinder{enabled: true, bizID: uuid.New(), bindErr: errors.New("db exploded")}
	consumer := service.NewTelegramOwnerLinkConsumer(binder)

	if err := consumer.HandleForTest(context.Background(), a2a.TelegramOwnerLink{Token: "t", FromID: 1}); err == nil {
		t.Fatal("an infrastructure bind error must be surfaced, not swallowed")
	}
}

// --- Subscribe fail-closed when handshake disabled --------------------------

func TestOwnerLinkConsumer_Subscribe_FailsClosedWhenDisabled(t *testing.T) {
	binder := &fakeOwnerLinkBinder{enabled: false, bizID: uuid.New()}
	consumer := service.NewTelegramOwnerLinkConsumer(binder)

	if _, err := consumer.Subscribe(nil); err == nil {
		t.Fatal("Subscribe must fail closed with a nil connection")
	}
}
