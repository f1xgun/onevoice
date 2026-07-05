package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

const testBotUsername = "onevoice_bot"

// --- fakes ------------------------------------------------------------------

// fakeLinkTokenStore is an in-memory single-use token store. It models the
// atomic consume: a token binds at most once; a second consume (or an expired /
// unknown one) returns ErrLinkTokenInvalid — identically, no enumeration.
type fakeLinkTokenStore struct {
	mu       sync.Mutex
	byHash   map[string]linkTokenRow
	inserted int
}

type linkTokenRow struct {
	businessID uuid.UUID
	expiresAt  time.Time
	consumed   bool
}

func newFakeLinkTokenStore() *fakeLinkTokenStore {
	return &fakeLinkTokenStore{byHash: map[string]linkTokenRow{}}
}

func (f *fakeLinkTokenStore) Insert(_ context.Context, businessID uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byHash[string(tokenHash)] = linkTokenRow{businessID: businessID, expiresAt: expiresAt}
	f.inserted++
	return nil
}

func (f *fakeLinkTokenStore) ConsumeAtomic(_ context.Context, tokenHash []byte) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.byHash[string(tokenHash)]
	if !ok || row.consumed || !row.expiresAt.After(time.Now()) {
		return uuid.Nil, domain.ErrLinkTokenInvalid
	}
	row.consumed = true
	f.byHash[string(tokenHash)] = row
	return row.businessID, nil
}

func (f *fakeLinkTokenStore) InvalidateAllForBusiness(_ context.Context, businessID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for h, row := range f.byHash {
		if row.businessID == businessID && !row.consumed {
			row.consumed = true
			f.byHash[h] = row
		}
	}
	return nil
}

// seedToken directly inserts a token row (bypassing Mint) so a test can control
// business binding, expiry, and consume state precisely.
func (f *fakeLinkTokenStore) seedToken(t *testing.T, plaintext string, businessID uuid.UUID, expiresAt time.Time) {
	t.Helper()
	sum := sha256.Sum256([]byte(plaintext))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byHash[string(sum[:])] = linkTokenRow{businessID: businessID, expiresAt: expiresAt}
}

// fakeLinkBinder records metadata writes per integration and returns a fixed
// integration set per business.
type fakeLinkBinder struct {
	mu         sync.Mutex
	byBusiness map[string][]domain.Integration
	updated    map[uuid.UUID]map[string]interface{}
}

func newFakeLinkBinder() *fakeLinkBinder {
	return &fakeLinkBinder{
		byBusiness: map[string][]domain.Integration{},
		updated:    map[uuid.UUID]map[string]interface{}{},
	}
}

func (f *fakeLinkBinder) ListByBusinessAndPlatform(_ context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if platform != a2a.AgentTelegram {
		return nil, nil
	}
	return f.byBusiness[businessID.String()], nil
}

func (f *fakeLinkBinder) UpdateMetadata(_ context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated[integrationID] = metadata
	return nil
}

func (f *fakeLinkBinder) lastMetadata(id uuid.UUID) (map[string]interface{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.updated[id]
	return m, ok
}

func (f *fakeLinkBinder) updateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updated)
}

func activeTgIntegration(businessID uuid.UUID, existingMeta map[string]interface{}) domain.Integration {
	if existingMeta == nil {
		existingMeta = map[string]interface{}{}
	}
	return domain.Integration{
		ID:         uuid.New(),
		BusinessID: businessID,
		Platform:   a2a.AgentTelegram,
		Status:     domain.IntegrationStatusActive,
		Metadata:   existingMeta,
	}
}

// --- Mint -------------------------------------------------------------------

func TestOwnerLink_Mint_ReturnsStartURL_TokenStoredHashedOnly(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()

	startURL, err := svc.Mint(context.Background(), bizID)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	prefix := "https://t.me/" + testBotUsername + "?start="
	if !strings.HasPrefix(startURL, prefix) {
		t.Fatalf("start URL missing expected prefix: %q", startURL)
	}
	token := strings.TrimPrefix(startURL, prefix)
	if len(token) != ownerLinkTokenLen {
		t.Fatalf("minted token length = %d, want %d", len(token), ownerLinkTokenLen)
	}

	// The store must never hold the plaintext — only its hash keys the map.
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, plaintextStored := store.byHash[token]; plaintextStored {
		t.Fatal("plaintext token must never be persisted; only its hash")
	}
	if store.inserted != 1 {
		t.Fatalf("expected exactly one token inserted, got %d", store.inserted)
	}
}

func TestOwnerLink_Mint_Disabled_WhenBotUsernameUnset(t *testing.T) {
	svc := NewTelegramOwnerLinkService(newFakeLinkTokenStore(), newFakeLinkBinder(), "")
	if svc.Enabled() {
		t.Fatal("service must be disabled when bot username is empty")
	}
	if _, err := svc.Mint(context.Background(), uuid.New()); err == nil {
		t.Fatal("Mint must fail closed when the handshake is disabled")
	}
}

// --- Bind: happy path -------------------------------------------------------

func TestOwnerLink_Bind_ValidToken_WritesVerifiedOwnerId_MergesMetadata(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()

	integ := activeTgIntegration(bizID, map[string]interface{}{"channel_title": "My Channel", "linked_chat_id": int64(42)})
	binder.byBusiness[bizID.String()] = []domain.Integration{integ}

	startURL, err := svc.Mint(context.Background(), bizID)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	const fromID = int64(123456789)
	gotBiz, err := svc.Bind(context.Background(), token, fromID, "owner_handle")
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if gotBiz != bizID {
		t.Fatalf("bound business mismatch: got %s want %s", gotBiz, bizID)
	}

	meta, ok := binder.lastMetadata(integ.ID)
	if !ok {
		t.Fatal("expected metadata written to the active integration")
	}
	if meta["telegram_user_id"] != "123456789" {
		t.Fatalf("telegram_user_id = %v, want the exact from.id string", meta["telegram_user_id"])
	}
	// Merge, not clobber: pre-existing keys survive.
	if meta["channel_title"] != "My Channel" || meta["linked_chat_id"] != int64(42) {
		t.Fatalf("bind must merge, not clobber, existing metadata: %+v", meta)
	}
}

// --- Threat: strict parse ---------------------------------------------------

func TestOwnerLink_Bind_MalformedToken_RejectedPreLookup(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)

	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 32)) // length 43
	cases := map[string]string{
		"empty":          "",
		"too_short":      valid[:10],
		"too_long":       valid + "AAAA",
		"padding_char":   strings.Repeat("A", ownerLinkTokenLen-1) + "=",
		"space":          strings.Repeat("A", ownerLinkTokenLen-1) + " ",
		"slash_charset":  strings.Repeat("A", ownerLinkTokenLen-1) + "/",
		"plus_charset":   strings.Repeat("A", ownerLinkTokenLen-1) + "+",
		"path_traversal": strings.Repeat("A", ownerLinkTokenLen-2) + "..",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Bind(context.Background(), tok, 555, "u")
			if err == nil {
				t.Fatalf("malformed token %q must be rejected", tok)
			}
			if binder.updateCount() != 0 {
				t.Fatal("malformed token must never write metadata")
			}
		})
	}
}

// --- Threat: single-use -----------------------------------------------------

func TestOwnerLink_Bind_SingleUse_SecondStartNoOp(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()
	integ := activeTgIntegration(bizID, nil)
	binder.byBusiness[bizID.String()] = []domain.Integration{integ}

	startURL, _ := svc.Mint(context.Background(), bizID)
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	if _, err := svc.Bind(context.Background(), token, 111, "u"); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if _, err := svc.Bind(context.Background(), token, 222, "u"); err != domain.ErrLinkTokenInvalid {
		t.Fatalf("second bind must be ErrLinkTokenInvalid (single-use), got %v", err)
	}
	// The first bind's owner id must stand; the second must not overwrite it.
	meta, _ := binder.lastMetadata(integ.ID)
	if meta["telegram_user_id"] != "111" {
		t.Fatalf("second bind must not rebind: telegram_user_id = %v", meta["telegram_user_id"])
	}
}

// --- Threat: expired --------------------------------------------------------

func TestOwnerLink_Bind_ExpiredToken_NeverBinds(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()
	binder.byBusiness[bizID.String()] = []domain.Integration{activeTgIntegration(bizID, nil)}

	token := base64.RawURLEncoding.EncodeToString(bytesSeq(1))
	store.seedToken(t, token, bizID, time.Now().Add(-time.Minute)) // already expired

	if _, err := svc.Bind(context.Background(), token, 111, "u"); err != domain.ErrLinkTokenInvalid {
		t.Fatalf("expired token must return ErrLinkTokenInvalid, got %v", err)
	}
	if binder.updateCount() != 0 {
		t.Fatal("expired token must never write metadata")
	}
}

// --- Threat: business-bound (leaked token cannot bind another business) -----

func TestOwnerLink_Bind_BusinessBound_LeakedTokenCannotBindOtherBusiness(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)

	bizA := uuid.New()
	bizB := uuid.New()
	integA := activeTgIntegration(bizA, nil)
	integB := activeTgIntegration(bizB, nil)
	binder.byBusiness[bizA.String()] = []domain.Integration{integA}
	binder.byBusiness[bizB.String()] = []domain.Integration{integB}

	// Token is minted for A only.
	startURL, _ := svc.Mint(context.Background(), bizA)
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	gotBiz, err := svc.Bind(context.Background(), token, 999, "u")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if gotBiz != bizA {
		t.Fatalf("token minted for A must bind A, got %s", gotBiz)
	}
	if _, bound := binder.lastMetadata(integB.ID); bound {
		t.Fatal("a token minted for A must NEVER mutate business B's integration")
	}
	if _, bound := binder.lastMetadata(integA.ID); !bound {
		t.Fatal("business A's integration must be the bind target")
	}
}

// --- Threat: from.id int64 exactness past float64 precision -----------------

func TestOwnerLink_Bind_FromIDInt64Exact_NoFloatPrecisionLoss(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()
	integ := activeTgIntegration(bizID, nil)
	binder.byBusiness[bizID.String()] = []domain.Integration{integ}

	startURL, _ := svc.Mint(context.Background(), bizID)
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	// > 2^53, where a float64 round-trip would lose the low bits.
	const bigID = int64(9007199254740993)
	if _, err := svc.Bind(context.Background(), token, bigID, "u"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	meta, _ := binder.lastMetadata(integ.ID)
	if meta["telegram_user_id"] != "9007199254740993" {
		t.Fatalf("large from.id must bind byte-exact, got %v", meta["telegram_user_id"])
	}
}

// --- Threat: unknown token / no leak ----------------------------------------

func TestOwnerLink_Bind_UnknownToken_NoBindingNoLeak(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)

	token := base64.RawURLEncoding.EncodeToString(bytesSeq(7)) // valid shape, never minted
	if _, err := svc.Bind(context.Background(), token, 111, "u"); err != domain.ErrLinkTokenInvalid {
		t.Fatalf("unknown token must return ErrLinkTokenInvalid, got %v", err)
	}
	if binder.updateCount() != 0 {
		t.Fatal("unknown token must never write metadata")
	}
}

// --- Threat: zero from.id ---------------------------------------------------

func TestOwnerLink_Bind_ZeroFromID_Rejected(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()
	binder.byBusiness[bizID.String()] = []domain.Integration{activeTgIntegration(bizID, nil)}

	startURL, _ := svc.Mint(context.Background(), bizID)
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	if _, err := svc.Bind(context.Background(), token, 0, "u"); err != domain.ErrLinkTokenInvalid {
		t.Fatalf("zero from.id must be rejected, got %v", err)
	}
	if binder.updateCount() != 0 {
		t.Fatal("zero from.id must never write metadata")
	}
}

// --- Threat: only ACTIVE integration is the bind target ---------------------

func TestOwnerLink_Bind_RevokedIntegration_NoBindTarget(t *testing.T) {
	store := newFakeLinkTokenStore()
	binder := newFakeLinkBinder()
	svc := NewTelegramOwnerLinkService(store, binder, testBotUsername)
	bizID := uuid.New()

	revoked := activeTgIntegration(bizID, nil)
	revoked.Status = domain.IntegrationStatusTokenExpired
	binder.byBusiness[bizID.String()] = []domain.Integration{revoked}

	startURL, _ := svc.Mint(context.Background(), bizID)
	token := strings.TrimPrefix(startURL, "https://t.me/"+testBotUsername+"?start=")

	_, err := svc.Bind(context.Background(), token, 111, "u")
	if err != domain.ErrIntegrationNotFound {
		t.Fatalf("a business with no active telegram integration must not bind, got %v", err)
	}
	if binder.updateCount() != 0 {
		t.Fatal("no metadata may be written when there is no active integration")
	}
}

// bytesSeq returns a 32-byte slice seeded by n so distinct tests produce
// distinct valid-shape tokens.
func bytesSeq(n byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = n + byte(i)
	}
	return b
}
