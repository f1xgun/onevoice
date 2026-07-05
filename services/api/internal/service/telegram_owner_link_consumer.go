package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// ownerLinkHandleTimeout bounds one end-to-end /start handshake handle
// (parse → consume token → load integration → bind). It runs on a fresh context
// (the NATS message carries none) so a slow DB step cannot wedge the
// subscription callback goroutine.
const ownerLinkHandleTimeout = 15 * time.Second

// ownerLinkBinder is the narrow slice of the owner-link service the consumer
// needs: validate a /start token and bind the authentic from.id. Kept as an
// interface so the consumer's tests inject a fake. *TelegramOwnerLinkService
// satisfies it.
type ownerLinkBinder interface {
	Enabled() bool
	Bind(ctx context.Context, token string, fromID int64, username string) (uuid.UUID, error)
}

// TelegramOwnerLinkConsumer subscribes to the /start deep-link handshake plane
// and binds the authentic tapper as the business's verified Telegram owner. It
// NEVER trusts the report on its own: the token must validate (single-use,
// unexpired, business-bound) inside the service before any binding, and the bound
// owner id is exactly the reported message.from.id. See
// docs/services/telegram-approval.md.
type TelegramOwnerLinkConsumer struct {
	links ownerLinkBinder
}

// NewTelegramOwnerLinkConsumer constructs the consumer.
func NewTelegramOwnerLinkConsumer(links ownerLinkBinder) *TelegramOwnerLinkConsumer {
	if links == nil {
		panic("NewTelegramOwnerLinkConsumer: links cannot be nil")
	}
	return &TelegramOwnerLinkConsumer{links: links}
}

// Subscribe attaches the consumer to a2a.TelegramOwnerLinkSubject on nc and
// returns an unsubscribe func. It refuses to subscribe (returns an error) when
// the handshake is disabled (no bot username configured), so an unconfigured
// deployment never exposes a bind path. Each message is handled on a fresh,
// bounded context in the subscription callback goroutine.
func (c *TelegramOwnerLinkConsumer) Subscribe(nc *natslib.Conn) (func(), error) {
	if nc == nil {
		return nil, errors.New("telegram owner-link consumer: nil NATS connection")
	}
	if !c.links.Enabled() {
		return nil, errors.New("telegram owner-link consumer: bot username unset — owner-link handshake disabled")
	}
	sub, err := nc.Subscribe(a2a.TelegramOwnerLinkSubject, func(msg *natslib.Msg) {
		var link a2a.TelegramOwnerLink
		if err := json.Unmarshal(msg.Data, &link); err != nil {
			slog.Warn("telegram owner-link consumer: dropping malformed handshake payload", "error", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), ownerLinkHandleTimeout)
		defer cancel()
		if err := c.handle(ctx, link); err != nil {
			slog.WarnContext(ctx, "telegram owner-link consumer: handle failed", "error", err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", a2a.TelegramOwnerLinkSubject, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

// handle validates the token and binds the authentic owner id. It is unit-tested
// directly. Every bad-token path (missing, malformed, expired, consumed, unknown
// business) is a SAFE no-op that returns nil — the handshake must never leak why
// it was declined and a decline is not an error. Only an infrastructure failure
// worth logging returns a non-nil error.
func (c *TelegramOwnerLinkConsumer) handle(ctx context.Context, link a2a.TelegramOwnerLink) error {
	businessID, err := c.links.Bind(ctx, link.Token, link.FromID, link.Username)
	if err != nil {
		if errors.Is(err, domain.ErrLinkTokenInvalid) || errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil
		}
		return fmt.Errorf("bind owner link: %w", err)
	}
	slog.InfoContext(ctx, "telegram owner-link: verified owner bound", "business_id", businessID)
	return nil
}
