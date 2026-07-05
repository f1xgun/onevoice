package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// ownerLinkTokenTTL bounds how long a minted /start deep-link stays valid. Short
// by design — a leaked link is inert after this window, which (with admin-only
// mint + single-use) bounds the documented first-tapper-wins residual.
const ownerLinkTokenTTL = 10 * time.Minute

// ownerLinkTokenLen is the exact plaintext length of a minted token:
// base64.RawURLEncoding of 32 random bytes. Parsing is length-strict — anything
// off this length is rejected before any DB lookup, so a malformed /start
// payload never touches the token store.
const ownerLinkTokenLen = 43

// telegramDeepLinkBase is the t.me deep-link root the /start owner-link URL is
// built on: https://t.me/<bot>?start=<token>.
const telegramDeepLinkBase = "https://t.me/"

// telegramLinkTokenStore is the persistence surface the owner-link service needs:
// mint a single-use hashed token bound to a business, and atomically consume one
// returning the bound business. Implemented by
// repository.TelegramOwnerLinkTokenRepository.
type telegramLinkTokenStore interface {
	Insert(ctx context.Context, businessID uuid.UUID, tokenHash []byte, expiresAt time.Time) error
	ConsumeAtomic(ctx context.Context, tokenHash []byte) (uuid.UUID, error)
	InvalidateAllForBusiness(ctx context.Context, businessID uuid.UUID) error
}

// telegramLinkIntegrationBinder is the integration slice the bind step needs:
// find the business's Telegram integrations and write the verified owner id onto
// the active one's metadata. *integrationService satisfies it.
type telegramLinkIntegrationBinder interface {
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
}

// TelegramOwnerLinkService mints and binds the /start deep-link owner-id
// handshake. It REPLACES the previous user-supplied telegram_user_id: the bound
// owner id is always message.from.id, which Telegram guarantees is the authentic
// id of the account that tapped the link — never a client-supplied value that
// could be mistyped (self-lockout) or spoofed.
//
// Security properties (each covered by a test):
//   - Token is crypto-random 256-bit, stored SHA-256-hashed (BYTEA UNIQUE);
//     plaintext never persists. Strict length parse before any lookup.
//   - Single-use + short-TTL + business-bound: enforced by the store's atomic
//     consume (see repository.TelegramOwnerLinkTokenRepository.ConsumeAtomic).
//   - Only an authenticated admin of the business may MINT (enforced at the
//     handler: RequireBusinessAccess + PermIntegrationsConnect; business_id comes
//     from BusinessContext, never the request body).
//   - Fail-closed: no/invalid/expired/consumed token → no binding, no leak.
type TelegramOwnerLinkService struct {
	tokens      telegramLinkTokenStore
	binder      telegramLinkIntegrationBinder
	botUsername string
}

// NewTelegramOwnerLinkService constructs the service. A blank botUsername means
// the plane is disabled fail-closed — Mint refuses so no unusable link is minted
// and the /start bind path is never reachable via a rendered URL.
func NewTelegramOwnerLinkService(tokens telegramLinkTokenStore, binder telegramLinkIntegrationBinder, botUsername string) *TelegramOwnerLinkService {
	if tokens == nil {
		panic("NewTelegramOwnerLinkService: tokens cannot be nil")
	}
	if binder == nil {
		panic("NewTelegramOwnerLinkService: binder cannot be nil")
	}
	return &TelegramOwnerLinkService{
		tokens:      tokens,
		binder:      binder,
		botUsername: botUsername,
	}
}

// Enabled reports whether the handshake is configured (a bot username is set).
// The handler gates the mint endpoint on this so an unconfigured deployment
// returns a clean "not available" rather than minting a dead link.
func (s *TelegramOwnerLinkService) Enabled() bool {
	return s.botUsername != ""
}

// Mint issues a single-use, short-TTL deep-link for the business to verify its
// Telegram owner. It invalidates any prior outstanding link for the business
// first (at most one live link at a time), then inserts a fresh hashed token and
// returns the tap URL https://t.me/<bot>?start=<token>. businessID is supplied by
// the caller from BusinessContext — never from request input.
func (s *TelegramOwnerLinkService) Mint(ctx context.Context, businessID uuid.UUID) (string, error) {
	if !s.Enabled() {
		return "", domain.ErrLinkTokenInvalid
	}
	if businessID == uuid.Nil {
		return "", fmt.Errorf("business id is required")
	}

	if err := s.tokens.InvalidateAllForBusiness(ctx, businessID); err != nil {
		return "", fmt.Errorf("invalidate prior owner-link tokens: %w", err)
	}

	plaintext, hash, err := generateOpaqueToken()
	if err != nil {
		return "", fmt.Errorf("generate owner-link token: %w", err)
	}
	if err := s.tokens.Insert(ctx, businessID, hash, time.Now().Add(ownerLinkTokenTTL)); err != nil {
		return "", fmt.Errorf("insert owner-link token: %w", err)
	}

	return startDeepLinkURL(s.botUsername, plaintext), nil
}

// Bind validates a /start token and writes fromID as the VERIFIED owner
// telegram_user_id on the token's bound business's active Telegram integration.
// It FAILS CLOSED and NEVER leaks why on any bad path:
//   - strict length parse rejects a malformed payload before any DB lookup;
//   - ConsumeAtomic collapses expired | consumed | unknown → ErrLinkTokenInvalid;
//   - the business is the one the token was minted for (returned by the store),
//     never anything the agent supplied, so a leaked token cannot bind another
//     business;
//   - fromID is written as the exact int64 (base-10 string), so a large id past
//     float64 precision round-trips byte-for-byte.
//
// A binding is idempotent-by-consume: the token is already consumed by the time
// the metadata write runs, so a duplicate report for the same token is a no-op
// (ErrLinkTokenInvalid on the second consume). Returns the bound businessID on
// success for the caller to log; a bad token returns ErrLinkTokenInvalid.
func (s *TelegramOwnerLinkService) Bind(ctx context.Context, token string, fromID int64, username string) (uuid.UUID, error) {
	if len(token) != ownerLinkTokenLen || !isBase64URL(token) {
		return uuid.Nil, domain.ErrLinkTokenInvalid
	}
	if fromID == 0 {
		return uuid.Nil, domain.ErrLinkTokenInvalid
	}

	sum := sha256.Sum256([]byte(token))
	businessID, err := s.tokens.ConsumeAtomic(ctx, sum[:])
	if err != nil {
		if errors.Is(err, domain.ErrLinkTokenInvalid) {
			return uuid.Nil, domain.ErrLinkTokenInvalid
		}
		return uuid.Nil, fmt.Errorf("consume owner-link token: %w", err)
	}

	integrations, err := s.binder.ListByBusinessAndPlatform(ctx, businessID, a2a.AgentTelegram)
	if err != nil {
		return uuid.Nil, fmt.Errorf("load telegram integrations for owner bind: %w", err)
	}

	target := activeTelegramIntegration(integrations)
	if target == nil {
		return businessID, domain.ErrIntegrationNotFound
	}

	metadata := mergeOwnerID(target.Metadata, fromID)
	if err := s.binder.UpdateMetadata(ctx, target.ID, metadata); err != nil {
		return uuid.Nil, fmt.Errorf("bind verified owner id: %w", err)
	}
	return businessID, nil
}

// activeTelegramIntegration returns the first ACTIVE Telegram integration, or nil
// if none is active. The verified owner id is only meaningful for a live channel,
// so a revoked/disconnected integration is never the bind target (mirrors the
// approval owner-authorization guard).
func activeTelegramIntegration(integrations []domain.Integration) *domain.Integration {
	for i := range integrations {
		if integrations[i].Status == domain.IntegrationStatusActive {
			return &integrations[i]
		}
	}
	return nil
}

// mergeOwnerID copies existing metadata and sets telegram_user_id to the exact
// int64 as a base-10 string, so channel_title / linked_chat_id and any other keys
// survive and no float64 precision loss can corrupt a large id.
func mergeOwnerID(existing map[string]interface{}, fromID int64) map[string]interface{} {
	metadata := make(map[string]interface{}, len(existing)+1)
	for k, v := range existing {
		metadata[k] = v
	}
	metadata["telegram_user_id"] = strconv.FormatInt(fromID, 10)
	return metadata
}

// startDeepLinkURL renders the Telegram deep-link a tap on which delivers
// "/start <token>" to the bot. The token is base64url (Telegram's start-param
// charset), so no escaping is needed.
func startDeepLinkURL(botUsername, token string) string {
	return telegramDeepLinkBase + botUsername + "?start=" + token
}

// isBase64URL reports whether s consists only of the RawURLEncoding alphabet
// (A–Z a–z 0–9 - _). Rejects any padding, whitespace, or other charset so a
// crafted payload cannot smuggle bytes past the length check.
func isBase64URL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
