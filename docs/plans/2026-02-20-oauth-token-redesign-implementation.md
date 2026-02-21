# OAuth Token Architecture Redesign — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded agent tokens with a proper OAuth-based, multi-user, multi-account integration system where users click "Connect", authorize on the platform, and tokens are managed server-side.

**Architecture:** API serves as the integration gateway — managing OAuth flows, encrypting tokens at rest, and serving them to agents via an internal mTLS endpoint. Agents no longer read tokens from env vars; they fetch per-request tokens from the API using a shared `pkg/tokenclient` package. The orchestrator receives dynamic business context from the API (which proxies chat requests), and propagates `BusinessID` to agents via context.

**Tech Stack:** Go 1.24, chi/v5, pgx/v5, Redis, AES-256-GCM encryption, mTLS (TLS 1.3), Next.js 14/React 18, axios

**Worktree:** `/Users/f1xgun/onevoice/.worktrees/oauth-token-redesign` (branch: `feature/oauth-token-redesign`)

**Run tests:** `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./pkg/... ./services/api/... ./services/orchestrator/... -v`

---

## Task 1: Database Migration — Multi-Account Unique Constraint

**Files:**
- Create: `migrations/postgres/000002_multi_account_integrations.up.sql`
- Create: `migrations/postgres/000002_multi_account_integrations.down.sql`

**Step 1: Create up migration**

```sql
-- migrations/postgres/000002_multi_account_integrations.up.sql
ALTER TABLE integrations DROP CONSTRAINT integrations_business_id_platform_key;
ALTER TABLE integrations ADD CONSTRAINT unique_business_platform_external
    UNIQUE (business_id, platform, external_id);
```

**Step 2: Create down migration**

```sql
-- migrations/postgres/000002_multi_account_integrations.down.sql
ALTER TABLE integrations DROP CONSTRAINT unique_business_platform_external;
ALTER TABLE integrations ADD CONSTRAINT integrations_business_id_platform_key
    UNIQUE (business_id, platform);
```

**Step 3: Commit**

```bash
git add migrations/postgres/000002_multi_account_integrations.up.sql migrations/postgres/000002_multi_account_integrations.down.sql
git commit -m "feat(db): add multi-account unique constraint (business_id, platform, external_id)"
```

---

## Task 2: BusinessID Context Helpers (pkg/a2a)

**Files:**
- Create: `pkg/a2a/context.go`
- Create: `pkg/a2a/context_test.go`

**Step 1: Write the failing test**

```go
// pkg/a2a/context_test.go
package a2a

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithBusinessID_RoundTrip(t *testing.T) {
	ctx := WithBusinessID(context.Background(), "biz-123")
	got := BusinessIDFromContext(ctx)
	assert.Equal(t, "biz-123", got)
}

func TestBusinessIDFromContext_Missing(t *testing.T) {
	got := BusinessIDFromContext(context.Background())
	assert.Equal(t, "", got)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./pkg/a2a/ -run TestWithBusinessID -v`
Expected: FAIL — `WithBusinessID` undefined

**Step 3: Write minimal implementation**

```go
// pkg/a2a/context.go
package a2a

import "context"

type ctxKey int

const businessIDKey ctxKey = iota

// WithBusinessID attaches a business ID to the context.
func WithBusinessID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, businessIDKey, id)
}

// BusinessIDFromContext extracts the business ID from context.
// Returns "" if not set.
func BusinessIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(businessIDKey).(string)
	return v
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./pkg/a2a/ -run TestWithBusinessID -v && go test ./pkg/a2a/ -run TestBusinessIDFromContext -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/a2a/context.go pkg/a2a/context_test.go
git commit -m "feat(a2a): add BusinessID context helpers"
```

---

## Task 3: Repository — Add Multi-Account Query Methods

**Files:**
- Modify: `pkg/domain/repository.go` (add 2 methods to `IntegrationRepository` interface)
- Modify: `services/api/internal/repository/integration.go` (implement them)
- Modify: `services/api/internal/repository/integration_test.go` (add tests)

**Step 1: Add interface methods to `pkg/domain/repository.go`**

Add these two methods to the `IntegrationRepository` interface:

```go
ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]Integration, error)
GetByBusinessPlatformExternal(ctx context.Context, businessID uuid.UUID, platform string, externalID string) (*Integration, error)
```

**Step 2: Write the failing tests**

Add tests in `services/api/internal/repository/integration_test.go`:

- `TestListByBusinessAndPlatform`: Creates 2 integrations with same business+platform but different external_ids. Verifies both returned, other platforms excluded.
- `TestGetByBusinessPlatformExternal_Found`: Creates integration with specific external_id. Verifies exact match found.
- `TestGetByBusinessPlatformExternal_NotFound`: Queries non-existent external_id. Verifies `domain.ErrIntegrationNotFound` returned.

These tests follow the existing test patterns in the file using pgx testcontainers or mock pool.

**Step 3: Run tests to verify they fail**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/repository/ -run "TestListByBusinessAndPlatform|TestGetByBusinessPlatformExternal" -v`
Expected: Compilation failure — methods not implemented

**Step 4: Implement the methods**

In `services/api/internal/repository/integration.go`, add:

```go
func (r *integrationRepository) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"business_id": businessID, "platform": platform}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query integrations: %w", err)
	}
	defer rows.Close()

	integrations := make([]domain.Integration, 0)
	for rows.Next() {
		var integration domain.Integration
		err := rows.Scan(
			&integration.ID, &integration.BusinessID, &integration.Platform, &integration.Status,
			&integration.EncryptedAccessToken, &integration.EncryptedRefreshToken,
			&integration.ExternalID, &integration.Metadata, &integration.TokenExpiresAt,
			&integration.CreatedAt, &integration.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan integration: %w", err)
		}
		integrations = append(integrations, integration)
	}
	return integrations, rows.Err()
}

func (r *integrationRepository) GetByBusinessPlatformExternal(ctx context.Context, businessID uuid.UUID, platform string, externalID string) (*domain.Integration, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"business_id": businessID, "platform": platform, "external_id": externalID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var integration domain.Integration
	err = r.pool.QueryRow(ctx, sql, args...).Scan(
		&integration.ID, &integration.BusinessID, &integration.Platform, &integration.Status,
		&integration.EncryptedAccessToken, &integration.EncryptedRefreshToken,
		&integration.ExternalID, &integration.Metadata, &integration.TokenExpiresAt,
		&integration.CreatedAt, &integration.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}
	return &integration, nil
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/repository/ -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add pkg/domain/repository.go services/api/internal/repository/integration.go services/api/internal/repository/integration_test.go
git commit -m "feat(repo): add multi-account query methods (ListByBusinessAndPlatform, GetByBusinessPlatformExternal)"
```

---

## Task 4: Integration Service — Connect, GetDecryptedToken, RefreshToken

**Files:**
- Modify: `services/api/internal/service/integration.go` (add methods + encryptor dependency)
- Modify: `services/api/internal/service/integration_test.go` (add tests)

**Step 1: Write the failing tests**

Tests for:
- `TestConnect_Success`: Encrypts access+refresh tokens, creates Integration via repo. Verifies tokens are stored encrypted (not plaintext).
- `TestConnect_DuplicateExternalID`: Returns `ErrIntegrationExists` when same business+platform+external_id already exists.
- `TestGetDecryptedToken_Success`: Stores encrypted integration, calls GetDecryptedToken. Verifies decrypted plaintext returned.
- `TestGetDecryptedToken_Expired_RefreshSucceeds`: Token expired, refresh succeeds (mock HTTP), returns new decrypted token.
- `TestGetDecryptedToken_NotFound`: Returns `ErrIntegrationNotFound`.

Mock the repo interface. Use real `crypto.Encryptor` with a test key.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/service/ -run "TestConnect|TestGetDecryptedToken" -v`
Expected: FAIL — methods undefined

**Step 3: Write implementation**

Update `integrationService` struct to include `encryptor *crypto.Encryptor`:

```go
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID) error
	Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*TokenResponse, error)
}

type ConnectParams struct {
	BusinessID   uuid.UUID
	Platform     string
	ExternalID   string
	AccessToken  string
	RefreshToken string
	Metadata     map[string]interface{}
	ExpiresAt    *time.Time
}

type TokenResponse struct {
	IntegrationID uuid.UUID              `json:"integration_id"`
	Platform      string                 `json:"platform"`
	ExternalID    string                 `json:"external_id"`
	AccessToken   string                 `json:"access_token"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
}
```

`NewIntegrationService` takes `(repo domain.IntegrationRepository, enc *crypto.Encryptor)`.

`Connect`: encrypts access+refresh tokens using `enc.Encrypt()`, creates `domain.Integration`, calls `repo.Create()`.

`GetDecryptedToken`: calls `repo.GetByBusinessPlatformExternal()`, decrypts access token with `enc.Decrypt()`, returns `TokenResponse`. If `TokenExpiresAt` is past and refresh token exists, the method should attempt refresh (placeholder for now — returns `ErrTokenExpired`).

**Step 4: Run tests to verify they pass**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/service/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add services/api/internal/service/integration.go services/api/internal/service/integration_test.go
git commit -m "feat(service): add Connect and GetDecryptedToken with AES-GCM encryption"
```

---

## Task 5: OAuth State Service (Redis)

**Files:**
- Create: `services/api/internal/service/oauth.go`
- Create: `services/api/internal/service/oauth_test.go`

**Step 1: Write the failing tests**

Tests for:
- `TestGenerateState_StoresInRedis`: Generates state with user+platform metadata, verifies stored in Redis with TTL.
- `TestValidateState_Success`: Stores state, validates it, returns metadata. Verifies single-use (second validate fails).
- `TestValidateState_Expired`: State with past TTL returns error.
- `TestValidateState_Invalid`: Random state string returns error.

Use `miniredis` for testing (already in go.mod).

**Step 2: Run tests to verify they fail**

Expected: FAIL — package doesn't exist

**Step 3: Write implementation**

```go
// services/api/internal/service/oauth.go
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type OAuthStateData struct {
	UserID     uuid.UUID `json:"user_id"`
	BusinessID uuid.UUID `json:"business_id"`
	Platform   string    `json:"platform"`
}

type OAuthService struct {
	redis *redis.Client
}

func NewOAuthService(redisClient *redis.Client) *OAuthService {
	return &OAuthService{redis: redisClient}
}

const oauthStateTTL = 10 * time.Minute
const oauthStatePrefix = "oauth:state:"

func (s *OAuthService) GenerateState(ctx context.Context, data OAuthStateData) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	state := hex.EncodeToString(b)

	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal state data: %w", err)
	}

	if err := s.redis.Set(ctx, oauthStatePrefix+state, payload, oauthStateTTL).Err(); err != nil {
		return "", fmt.Errorf("store state: %w", err)
	}

	return state, nil
}

func (s *OAuthService) ValidateState(ctx context.Context, state string) (*OAuthStateData, error) {
	key := oauthStatePrefix + state
	payload, err := s.redis.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("invalid or expired oauth state")
		}
		return nil, fmt.Errorf("get state: %w", err)
	}

	var data OAuthStateData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("unmarshal state data: %w", err)
	}

	return &data, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/service/ -run "TestGenerateState|TestValidateState" -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add services/api/internal/service/oauth.go services/api/internal/service/oauth_test.go
git commit -m "feat(service): add OAuth state management with Redis (single-use, 10min TTL)"
```

---

## Task 6: API Config — Add OAuth Environment Variables

**Files:**
- Modify: `services/api/internal/config/config.go`

**Step 1: Add OAuth config fields**

Add to `Config` struct:

```go
// OAuth credentials
VKClientID          string
VKClientSecret      string
VKRedirectURI       string
YandexClientID      string
YandexClientSecret  string
YandexRedirectURI   string
TelegramBotToken    string

// Internal server
InternalPort string

// Orchestrator
OrchestratorURL string
```

Update `Load()` to read these from env:

```go
VKClientID:          os.Getenv("VK_CLIENT_ID"),
VKClientSecret:      os.Getenv("VK_CLIENT_SECRET"),
VKRedirectURI:       getEnv("VK_REDIRECT_URI", "http://localhost/api/v1/oauth/vk/callback"),
YandexClientID:      os.Getenv("YANDEX_CLIENT_ID"),
YandexClientSecret:  os.Getenv("YANDEX_CLIENT_SECRET"),
YandexRedirectURI:   getEnv("YANDEX_REDIRECT_URI", "http://localhost/api/v1/oauth/yandex_business/callback"),
TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
InternalPort:        getEnv("INTERNAL_PORT", "8443"),
OrchestratorURL:     getEnv("ORCHESTRATOR_URL", "http://localhost:8090"),
```

No validation on OAuth fields — they're optional (platform-specific connections won't work without them, but API still starts).

**Step 2: Commit**

```bash
git add services/api/internal/config/config.go
git commit -m "feat(config): add OAuth, Telegram, internal port, and orchestrator URL config"
```

---

## Task 7: Internal Token Handler

**Files:**
- Create: `services/api/internal/handler/internal_token.go`
- Create: `services/api/internal/handler/internal_token_test.go`

**Step 1: Write the failing tests**

Tests for:
- `TestGetToken_Success`: Mock integration service returns decrypted token. Verify JSON response shape.
- `TestGetToken_MissingBusinessID`: Returns 400.
- `TestGetToken_MissingPlatform`: Returns 400.
- `TestGetToken_NotFound`: Returns 404.
- `TestGetToken_Expired`: Returns 410 when `ErrTokenExpired`.

**Step 2: Run tests to verify they fail**

Expected: FAIL — file doesn't exist

**Step 3: Write implementation**

```go
// services/api/internal/handler/internal_token.go
package handler

import (
	"errors"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
)

type InternalTokenHandler struct {
	integrationService IntegrationService
}

func NewInternalTokenHandler(integrationService IntegrationService) *InternalTokenHandler {
	return &InternalTokenHandler{integrationService: integrationService}
}

func (h *InternalTokenHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	businessIDStr := r.URL.Query().Get("business_id")
	platform := r.URL.Query().Get("platform")
	externalID := r.URL.Query().Get("external_id")

	if businessIDStr == "" || platform == "" {
		writeJSONError(w, http.StatusBadRequest, "business_id and platform are required")
		return
	}

	businessID, err := uuid.Parse(businessIDStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid business_id")
		return
	}

	token, err := h.integrationService.GetDecryptedToken(r.Context(), businessID, platform, externalID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			writeJSONError(w, http.StatusNotFound, "integration not found")
			return
		}
		if errors.Is(err, domain.ErrTokenExpired) {
			writeJSONError(w, http.StatusGone, "token expired, refresh failed")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, token)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/handler/ -run TestGetToken -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add services/api/internal/handler/internal_token.go services/api/internal/handler/internal_token_test.go
git commit -m "feat(handler): add internal token endpoint for agent token fetching"
```

---

## Task 8: OAuth Handlers — VK, Telegram, Yandex.Business

**Files:**
- Create: `services/api/internal/handler/oauth.go`
- Create: `services/api/internal/handler/oauth_test.go`

This is the largest handler task. It implements:

### VK OAuth

**`GetVKAuthURL(w, r)`** — Protected route (JWT required):
1. Get user ID from context, look up business
2. Call `oauthService.GenerateState()` with user+business+platform
3. Build VK OAuth URL: `https://oauth.vk.com/authorize?client_id={id}&redirect_uri={uri}&scope=wall,groups,manage&response_type=code&state={state}&v=5.199`
4. Return JSON: `{"url": "<oauth_url>"}`

**`VKCallback(w, r)`** — Public route (no JWT, state validates):
1. Extract `code` and `state` from query params
2. `oauthService.ValidateState(state)` → get user+business+platform
3. Exchange code for token: POST to `https://oauth.vk.com/access_token` with `client_id`, `client_secret`, `redirect_uri`, `code`
4. Call VK API `groups.get?filter=admin,editor&access_token={token}` to get manageable groups
5. For each group: `integrationService.Connect(ConnectParams{BusinessID, Platform: "vk", ExternalID: groupID, AccessToken: token})`
6. Redirect browser to `/integrations?connected=vk`

### Telegram

**`VerifyTelegramLogin(w, r)`** — Protected route:
1. Verify Telegram Login Widget hash: HMAC-SHA256 with `SHA256(bot_token)` as key
2. Check `auth_date` < 5 minutes
3. Return 200 with verified user info

**`ConnectTelegram(w, r)`** — Protected route:
1. Accept `{channel_id, telegram_user_id}`
2. Verify bot is admin in channel (via Telegram API `getChatAdministrators`)
3. Call `integrationService.Connect(ConnectParams{BusinessID, Platform: "telegram", ExternalID: channelID, AccessToken: cfg.TelegramBotToken, Metadata: {"telegram_user_id": "...", "channel_title": "..."}})`
4. Return created integration

### Yandex.Business

**`GetYandexAuthURL(w, r)`** — Protected route:
1. Generate state
2. Build URL: `https://oauth.yandex.ru/authorize?response_type=code&client_id={id}&redirect_uri={uri}&state={state}`
3. Return JSON: `{"url": "<oauth_url>"}`

**`YandexCallback(w, r)`** — Public route:
1. Validate state
2. Exchange code: POST `https://oauth.yandex.net/token` with `grant_type=authorization_code&code={code}&client_id={id}&client_secret={secret}`
3. Store tokens with `integrationService.Connect()`. Note: cookie extraction deferred to agent-side for now (complexity reduction). Store Yandex OAuth token; agent uses it to get cookies on-demand.
4. Redirect to `/integrations?connected=yandex_business`

**Step 1: Write tests**

Use `httptest` server to mock VK/Yandex/Telegram HTTP APIs. Test the handlers with:
- `TestGetVKAuthURL_ReturnsURL`: Verify URL contains client_id and state
- `TestVKCallback_ExchangesCode`: Mock VK token exchange, verify integration created
- `TestVKCallback_InvalidState`: Returns redirect to error page
- `TestVerifyTelegramLogin_ValidHash`: Verify HMAC check passes
- `TestVerifyTelegramLogin_ExpiredAuthDate`: Returns 401
- `TestConnectTelegram_BotNotAdmin`: Returns 400
- `TestGetYandexAuthURL_ReturnsURL`: Verify URL contains client_id
- `TestYandexCallback_ExchangesCode`: Mock Yandex token exchange, verify integration created

**Step 2: Implement**

Create `OAuthHandler` struct with dependencies:
```go
type OAuthHandler struct {
	oauthService       *OAuthService
	integrationService IntegrationService
	businessService    BusinessService
	cfg                OAuthConfig
	httpClient         *http.Client // for external API calls (testable)
}

type OAuthConfig struct {
	VKClientID         string
	VKClientSecret     string
	VKRedirectURI      string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
	TelegramBotToken   string
}
```

Note: Use an `*http.Client` field (instead of a `net/http` global) so tests can inject `httptest.Server` targets.

**Step 3: Commit**

```bash
git add services/api/internal/handler/oauth.go services/api/internal/handler/oauth_test.go
git commit -m "feat(handler): add OAuth handlers for VK, Telegram, and Yandex.Business"
```

---

## Task 9: Chat Proxy Handler

**Files:**
- Create: `services/api/internal/handler/chat_proxy.go`
- Create: `services/api/internal/handler/chat_proxy_test.go`

**Step 1: Write the failing tests**

- `TestChatProxy_EnrichesWithBusinessContext`: Verify the proxy adds business context (name, category, active integrations) to the request body before forwarding.
- `TestChatProxy_StreamsSSEResponse`: Mock orchestrator returns SSE events. Verify they're forwarded 1:1 to the client.
- `TestChatProxy_NoBusiness`: Returns 404.
- `TestChatProxy_OrchestratorDown`: Returns 502.

**Step 2: Implement**

```go
// services/api/internal/handler/chat_proxy.go
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/go-chi/chi/v5"
)

type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	orchestratorURL    string
	httpClient         *http.Client
}

func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	orchestratorURL string,
	httpClient *http.Client,
) *ChatProxyHandler {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ChatProxyHandler{
		businessService:    businessService,
		integrationService: integrationService,
		orchestratorURL:    orchestratorURL,
		httpClient:         httpClient,
	}
}

type chatProxyRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

type orchestratorRequest struct {
	Model              string                 `json:"model"`
	Message            string                 `json:"message"`
	BusinessID         string                 `json:"business_id"`
	BusinessName       string                 `json:"business_name"`
	BusinessCategory   string                 `json:"business_category"`
	BusinessAddress    string                 `json:"business_address"`
	BusinessPhone      string                 `json:"business_phone"`
	BusinessDesc       string                 `json:"business_description"`
	ActiveIntegrations []string               `json:"active_integrations"`
}

func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "conversationID")

	var req chatProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Look up business
	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found — create a business profile first")
			return
		}
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Get active integrations
	integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
	if err != nil {
		slog.Error("failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	activeIntegrations := make([]string, 0)
	seen := make(map[string]bool)
	for _, integ := range integrations {
		if integ.Status == "active" && !seen[integ.Platform] {
			activeIntegrations = append(activeIntegrations, integ.Platform)
			seen[integ.Platform] = true
		}
	}

	// Build enriched request for orchestrator
	orchReq := orchestratorRequest{
		Model:              req.Model,
		Message:            req.Message,
		BusinessID:         business.ID.String(),
		BusinessName:       business.Name,
		BusinessCategory:   business.Category,
		BusinessAddress:    business.Address,
		BusinessPhone:      business.Phone,
		BusinessDesc:       business.Description,
		ActiveIntegrations: activeIntegrations,
	}

	// Forward to orchestrator
	orchURL := fmt.Sprintf("%s/chat/%s", h.orchestratorURL, conversationID)
	body, _ := json.Marshal(orchReq)
	proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", orchURL, bytes.NewReader(body))
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		slog.Error("orchestrator request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer resp.Body.Close()

	// Stream SSE response back to client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
}
```

Note: Add `"bytes"` to imports.

**Step 3: Run tests**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/internal/handler/ -run TestChatProxy -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/api/internal/handler/chat_proxy.go services/api/internal/handler/chat_proxy_test.go
git commit -m "feat(handler): add chat proxy handler that enriches requests with business context"
```

---

## Task 10: Router — Add OAuth, Internal, and Chat Routes

**Files:**
- Modify: `services/api/internal/router/router.go`

**Step 1: Update Handlers struct and Setup function**

Add new handlers to the `Handlers` struct:

```go
type Handlers struct {
	Auth          *handler.AuthHandler
	Business      *handler.BusinessHandler
	Integration   *handler.IntegrationHandler
	Conversation  *handler.ConversationHandler
	OAuth         *handler.OAuthHandler
	InternalToken *handler.InternalTokenHandler
	ChatProxy     *handler.ChatProxyHandler
}
```

Add routes in `Setup()`:

```go
// Inside the /api/v1 route group:

// OAuth callback routes (public — state parameter validates session)
r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
r.Get("/oauth/yandex_business/callback", handlers.OAuth.YandexCallback)

// Inside the protected group:

// OAuth auth-url routes (need JWT to generate state with user context)
r.Get("/integrations/vk/auth-url", handlers.OAuth.GetVKAuthURL)
r.Get("/integrations/yandex_business/auth-url", handlers.OAuth.GetYandexAuthURL)

// Telegram routes
r.Post("/integrations/telegram/verify", handlers.OAuth.VerifyTelegramLogin)
r.Post("/integrations/telegram/connect", handlers.OAuth.ConnectTelegram)

// Chat proxy (replaces direct orchestrator access)
r.Post("/chat/{conversationID}", handlers.ChatProxy.Chat)

// Remove the old stub: r.Post("/integrations/{platform}/connect", ...)
```

Add internal router function:

```go
// SetupInternal creates the internal mTLS-protected router.
func SetupInternal(handlers *Handlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Get("/internal/v1/tokens", handlers.InternalToken.GetToken)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return r
}
```

**Step 2: Commit**

```bash
git add services/api/internal/router/router.go
git commit -m "feat(router): add OAuth, internal token, and chat proxy routes"
```

---

## Task 11: API Main — Wire Encryptor, OAuth, Internal Server

**Files:**
- Modify: `services/api/cmd/main.go`

**Step 1: Update wiring**

1. Keep the encryptor (currently created but unused) and pass it to `IntegrationService`:
   ```go
   enc, err := crypto.NewEncryptor([]byte(cfg.EncryptionKey))
   // ...
   integrationService := service.NewIntegrationService(integrationRepo, enc)
   ```

2. Create `OAuthService`:
   ```go
   oauthService := service.NewOAuthService(redisClient)
   ```

3. Create `OAuthHandler`:
   ```go
   oauthHandler := handler.NewOAuthHandler(oauthService, integrationService, businessService, handler.OAuthConfig{
       VKClientID: cfg.VKClientID, VKClientSecret: cfg.VKClientSecret, VKRedirectURI: cfg.VKRedirectURI,
       YandexClientID: cfg.YandexClientID, YandexClientSecret: cfg.YandexClientSecret, YandexRedirectURI: cfg.YandexRedirectURI,
       TelegramBotToken: cfg.TelegramBotToken,
   }, nil)
   ```

4. Create `InternalTokenHandler` and `ChatProxyHandler`:
   ```go
   internalTokenHandler := handler.NewInternalTokenHandler(integrationService)
   chatProxyHandler := handler.NewChatProxyHandler(businessService, integrationService, cfg.OrchestratorURL, nil)
   ```

5. Wire into `Handlers`:
   ```go
   handlers := &router.Handlers{
       Auth:          handler.NewAuthHandler(userService),
       Business:      handler.NewBusinessHandler(businessService),
       Integration:   handler.NewIntegrationHandler(integrationService, businessService),
       Conversation:  handler.NewConversationHandler(conversationRepo),
       OAuth:         oauthHandler,
       InternalToken: internalTokenHandler,
       ChatProxy:     chatProxyHandler,
   }
   ```

6. Add internal mTLS server (start as plain HTTP for now — mTLS will be layered on with certs):
   ```go
   // Internal server
   internalRouter := router.SetupInternal(handlers)
   internalAddr := ":" + cfg.InternalPort
   internalSrv := &http.Server{
       Addr:    internalAddr,
       Handler: internalRouter,
   }
   go func() {
       log.Info("internal server listening", "addr", internalAddr)
       if err := internalSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
           log.Error("internal server error", "error", err)
       }
   }()
   ```

   Add to graceful shutdown:
   ```go
   internalSrv.Shutdown(ctx)
   ```

**Step 2: Verify build**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go build ./services/api/...`
Expected: Success

**Step 3: Run full API test suite**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/api/... -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/api/cmd/main.go
git commit -m "feat(api): wire encryptor, OAuth service, internal server, and chat proxy"
```

---

## Task 12: Token Client Package

**Files:**
- Create: `pkg/tokenclient/client.go`
- Create: `pkg/tokenclient/client_test.go`

**Step 1: Write the failing tests**

Tests for:
- `TestGetToken_FetchesFromAPI`: Mock HTTP server returns token JSON. Verify deserialized correctly.
- `TestGetToken_CachesResult`: Two calls with same params. Verify HTTP server called only once.
- `TestGetToken_CacheEviction`: Set token with `ExpiresAt` in 30 seconds. Verify cache evicts early (when `expiresAt - now < 1 minute`).
- `TestGetToken_NotFound`: Server returns 404. Verify error.
- `TestGetToken_Gone`: Server returns 410. Verify error indicates token expired.

**Step 2: Run tests to verify they fail**

Expected: FAIL — package doesn't exist

**Step 3: Write implementation**

```go
// pkg/tokenclient/client.go
package tokenclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type TokenResponse struct {
	IntegrationID string                 `json:"integration_id"`
	Platform      string                 `json:"platform"`
	ExternalID    string                 `json:"external_id"`
	AccessToken   string                 `json:"access_token"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
}

type cacheEntry struct {
	token     *TokenResponse
	fetchedAt time.Time
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	cacheTTL   time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		cacheTTL:   5 * time.Minute,
		cache:      make(map[string]cacheEntry),
	}
}

func cacheKey(businessID, platform, externalID string) string {
	return businessID + ":" + platform + ":" + externalID
}

func (c *Client) GetToken(ctx context.Context, businessID, platform, externalID string) (*TokenResponse, error) {
	key := cacheKey(businessID, platform, externalID)

	// Check cache
	c.mu.RLock()
	if entry, ok := c.cache[key]; ok {
		// Evict if older than TTL or token expires within 1 minute
		if time.Since(entry.fetchedAt) < c.cacheTTL && !tokenExpiringSoon(entry.token) {
			c.mu.RUnlock()
			return entry.token, nil
		}
	}
	c.mu.RUnlock()

	// Fetch from API
	u := fmt.Sprintf("%s/internal/v1/tokens?business_id=%s&platform=%s&external_id=%s",
		c.baseURL,
		url.QueryEscape(businessID),
		url.QueryEscape(platform),
		url.QueryEscape(externalID),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("tokenclient: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tokenclient: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tokenclient: integration not found")
	}
	if resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("tokenclient: token expired and refresh failed")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tokenclient: unexpected status %d", resp.StatusCode)
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("tokenclient: decode response: %w", err)
	}

	// Cache the result
	c.mu.Lock()
	c.cache[key] = cacheEntry{token: &token, fetchedAt: time.Now()}
	c.mu.Unlock()

	return &token, nil
}

func tokenExpiringSoon(t *TokenResponse) bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Until(*t.ExpiresAt) < time.Minute
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./pkg/tokenclient/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add pkg/tokenclient/client.go pkg/tokenclient/client_test.go
git commit -m "feat(tokenclient): add mTLS-ready token client with in-memory caching"
```

---

## Task 13: Orchestrator — Accept Dynamic BusinessContext

**Files:**
- Modify: `services/orchestrator/internal/handler/chat.go`
- Modify: `services/orchestrator/internal/handler/chat_test.go`

**Step 1: Update chatRequest struct**

Replace the static `biz` field on `ChatHandler` with dynamic per-request context from the API proxy.

```go
type chatRequest struct {
	Model              string   `json:"model"`
	Message            string   `json:"message"`
	BusinessID         string   `json:"business_id"`
	BusinessName       string   `json:"business_name"`
	BusinessCategory   string   `json:"business_category"`
	BusinessAddress    string   `json:"business_address"`
	BusinessPhone      string   `json:"business_phone"`
	BusinessDesc       string   `json:"business_description"`
	ActiveIntegrations []string `json:"active_integrations"`
}
```

Update `ChatHandler` to no longer hold a static `biz`:

```go
type ChatHandler struct {
	runner Runner
}

func NewChatHandler(runner Runner) *ChatHandler {
	return &ChatHandler{runner: runner}
}
```

Update `Chat()` method to build `BusinessContext` from request:

```go
biz := prompt.BusinessContext{
	Name:               req.BusinessName,
	Category:           req.BusinessCategory,
	Address:            req.BusinessAddress,
	Phone:              req.BusinessPhone,
	Description:        req.BusinessDesc,
	ActiveIntegrations: req.ActiveIntegrations,
	Now:                time.Now(),
}

ctx := a2a.WithBusinessID(r.Context(), req.BusinessID)

runReq := orchestrator.RunRequest{
	Model:              req.Model,
	BusinessContext:    biz,
	ActiveIntegrations: req.ActiveIntegrations,
	Messages:           []llm.Message{{Role: "user", Content: req.Message}},
}

events, err := h.runner.Run(ctx, runReq)
```

**Step 2: Update tests**

Update existing `chat_test.go` to pass business fields in request JSON instead of static `biz` to `NewChatHandler`.

**Step 3: Verify**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/orchestrator/internal/handler/ -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/orchestrator/internal/handler/chat.go services/orchestrator/internal/handler/chat_test.go
git commit -m "feat(orchestrator): accept dynamic BusinessContext from API proxy per-request"
```

---

## Task 14: NATSExecutor — Propagate BusinessID from Context

**Files:**
- Modify: `services/orchestrator/internal/natsexec/executor.go`
- Modify: `services/orchestrator/internal/natsexec/executor_test.go`

**Step 1: Write test**

Add test: `TestExecute_SetsBusinessIDFromContext`
- Create context with `a2a.WithBusinessID(ctx, "biz-uuid")`
- Call `executor.Execute(ctx, args)`
- Capture the NATS request data, unmarshal to `ToolRequest`
- Assert `req.BusinessID == "biz-uuid"`

**Step 2: Update Execute()**

In `executor.go`, line 33:

```go
func (e *NATSExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := a2a.ToolRequest{
		TaskID:     uuid.New().String(),
		Tool:       e.agentID,
		Args:       args,
		BusinessID: a2a.BusinessIDFromContext(ctx),
	}
	// ... rest unchanged
```

**Step 3: Verify**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/orchestrator/internal/natsexec/ -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/orchestrator/internal/natsexec/executor.go services/orchestrator/internal/natsexec/executor_test.go
git commit -m "feat(natsexec): propagate BusinessID from context to ToolRequest"
```

---

## Task 15: Orchestrator Main — Remove Static Business Context

**Files:**
- Modify: `services/orchestrator/cmd/main.go`
- Modify: `services/orchestrator/internal/config/config.go`

**Step 1: Update cmd/main.go**

Remove static `biz` construction (lines 57-66) and pass only `orch` to `NewChatHandler`:

```go
chatHandler := handler.NewChatHandler(orch)
```

Remove: `BusinessName`, `BusinessCategory`, `BusinessTone`, `ActiveIntegrations` from config.

**Step 2: Update config.go**

Remove `BusinessName`, `BusinessCategory`, `BusinessTone`, `ActiveIntegrations` fields and their env reading. These are now provided per-request by the API proxy.

**Step 3: Verify**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/orchestrator/... -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/orchestrator/cmd/main.go services/orchestrator/internal/config/config.go
git commit -m "refactor(orchestrator): remove static business context, now provided per-request"
```

---

## Task 16: Agent-VK — Per-Request Token Fetch

**Files:**
- Modify: `services/agent-vk/internal/agent/handler.go`
- Modify: `services/agent-vk/internal/agent/handler_test.go`
- Modify: `services/agent-vk/cmd/main.go`
- Modify: `services/agent-vk/go.mod` (if needed for tokenclient dependency)

**Step 1: Update handler**

Change `Handler` to use a `TokenFetcher` interface instead of a static `VKClient`:

```go
type TokenFetcher interface {
	GetToken(ctx context.Context, businessID, platform, externalID string) (*tokenclient.TokenResponse, error)
}

type VKClientFactory func(accessToken string) VKClient

type Handler struct {
	tokens       TokenFetcher
	clientFactory VKClientFactory
}

func NewHandler(tokens TokenFetcher, factory VKClientFactory) *Handler {
	return &Handler{tokens: tokens, clientFactory: factory}
}
```

Each tool handler:
1. Extract `group_id` from `req.Args`
2. `token, err := h.tokens.GetToken(ctx, req.BusinessID, "vk", groupID)`
3. `client := h.clientFactory(token.AccessToken)`
4. Execute the VK API call with the per-request client

**Step 2: Update cmd/main.go**

Replace:
```go
accessToken := os.Getenv("VK_ACCESS_TOKEN")
// ...
client := vk.New(accessToken)
handler := agentpkg.NewHandler(client)
```

With:
```go
apiURL := getEnv("API_INTERNAL_URL", "http://localhost:8443")
tokenClient := tokenclient.New(apiURL, nil)
handler := agentpkg.NewHandler(tokenClient, func(token string) agentpkg.VKClient {
    return vk.New(token)
})
```

Remove `VK_ACCESS_TOKEN` requirement. Add `API_INTERNAL_URL` env var.

**Step 3: Verify**

Run: `cd /Users/f1xgun/onevoice/.worktrees/oauth-token-redesign && go test ./services/agent-vk/... -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add services/agent-vk/
git commit -m "feat(agent-vk): per-request token fetch via internal token API"
```

---

## Task 17: Agent-Telegram — Per-Request Token Fetch

**Files:**
- Modify: `services/agent-telegram/internal/agent/handler.go`
- Modify: `services/agent-telegram/internal/agent/handler_test.go`
- Modify: `services/agent-telegram/cmd/main.go`

Same pattern as Task 16. Change `Handler` to:

```go
type Handler struct {
	tokens        TokenFetcher
	senderFactory func(botToken string) (Sender, error)
}
```

Each tool handler:
1. Extract `channel_id` from `req.Args`
2. `token, err := h.tokens.GetToken(ctx, req.BusinessID, "telegram", channelID)`
3. `sender, err := h.senderFactory(token.AccessToken)`
4. Execute with per-request sender

In `cmd/main.go`:
```go
apiURL := getEnv("API_INTERNAL_URL", "http://localhost:8443")
tokenClient := tokenclient.New(apiURL, nil)
handler := agentpkg.NewHandler(tokenClient, func(botToken string) (agentpkg.Sender, error) {
    return telegram.New(botToken)
})
```

Remove `TELEGRAM_BOT_TOKEN` requirement from agent (it's now stored in API).

**Commit:**

```bash
git add services/agent-telegram/
git commit -m "feat(agent-telegram): per-request token fetch via internal token API"
```

---

## Task 18: Agent-Yandex-Business — Per-Request Cookie Fetch

**Files:**
- Modify: `services/agent-yandex-business/internal/agent/handler.go`
- Modify: `services/agent-yandex-business/internal/agent/handler_test.go`
- Modify: `services/agent-yandex-business/cmd/main.go`

Same pattern. Change `Handler` to:

```go
type Handler struct {
	tokens         TokenFetcher
	browserFactory func(cookiesJSON string) YandexBrowser
}
```

Each tool handler:
1. `token, err := h.tokens.GetToken(ctx, req.BusinessID, "yandex_business", "")`
2. Cookies stored in token's AccessToken or Metadata
3. `browser := h.browserFactory(token.AccessToken)`
4. Execute RPA operation

In `cmd/main.go`:
```go
apiURL := getEnv("API_INTERNAL_URL", "http://localhost:8443")
tokenClient := tokenclient.New(apiURL, nil)
handler := agentpkg.NewHandler(tokenClient, func(cookies string) agentpkg.YandexBrowser {
    return yandex.NewBrowser(cookies)
})
```

Remove `YANDEX_COOKIES_JSON` requirement.

**Commit:**

```bash
git add services/agent-yandex-business/
git commit -m "feat(agent-yandex-business): per-request cookie fetch via internal token API"
```

---

## Task 19: Makefile — Add Certs Target

**Files:**
- Modify: `Makefile`

**Step 1: Add certs target**

```makefile
.PHONY: certs

certs: ## Generate mTLS certificates for internal communication
	@echo "Generating certificates..."
	@mkdir -p certs
	@# CA
	@openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
		-days 3650 -nodes -keyout certs/ca.key -out certs/ca.crt \
		-subj "/CN=OneVoice Internal CA" 2>/dev/null
	@# Server (API internal)
	@openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
		-nodes -keyout certs/server.key -out certs/server.csr \
		-subj "/CN=api" 2>/dev/null
	@echo "subjectAltName=DNS:api,DNS:localhost,IP:127.0.0.1" > certs/server.ext
	@openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/server.crt -days 3650 -extfile certs/server.ext 2>/dev/null
	@rm certs/server.csr certs/server.ext
	@# Agent clients
	@for agent in telegram vk yandex-business; do \
		openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
			-nodes -keyout certs/$$agent.key -out certs/$$agent.csr \
			-subj "/CN=agent-$$agent" 2>/dev/null; \
		openssl x509 -req -in certs/$$agent.csr -CA certs/ca.crt -CAkey certs/ca.key \
			-CAcreateserial -out certs/$$agent.crt -days 3650 2>/dev/null; \
		rm certs/$$agent.csr; \
	done
	@rm -f certs/ca.srl
	@echo "Certificates generated in certs/"
```

**Step 2: Add certs/ to .gitignore**

Check if `certs/` is already in `.gitignore`. If not, add it.

**Step 3: Commit**

```bash
git add Makefile .gitignore
git commit -m "feat(makefile): add certs target for mTLS certificate generation"
```

---

## Task 20: Docker Compose — Cert Volumes, Remove Token Env Vars

**Files:**
- Modify: `docker-compose.yml`

**Step 1: Update agent services**

Remove hardcoded token env vars from agents:

```yaml
# agent-telegram: remove TELEGRAM_BOT_TOKEN, add:
environment:
  NATS_URL: nats://nats:4222
  API_INTERNAL_URL: http://api:8443
volumes:
  - ./certs/ca.crt:/certs/ca.crt:ro
  - ./certs/telegram.crt:/certs/client.crt:ro
  - ./certs/telegram.key:/certs/client.key:ro

# agent-vk: remove VK_ACCESS_TOKEN, add:
environment:
  NATS_URL: nats://nats:4222
  API_INTERNAL_URL: http://api:8443
volumes:
  - ./certs/ca.crt:/certs/ca.crt:ro
  - ./certs/vk.crt:/certs/client.crt:ro
  - ./certs/vk.key:/certs/client.key:ro

# agent-yandex-business: remove YANDEX_COOKIES_JSON, add:
environment:
  NATS_URL: nats://nats:4222
  API_INTERNAL_URL: http://api:8443
volumes:
  - ./certs/ca.crt:/certs/ca.crt:ro
  - ./certs/yandex-business.crt:/certs/client.crt:ro
  - ./certs/yandex-business.key:/certs/client.key:ro
```

**Step 2: Update API service**

Add OAuth env vars and internal port:

```yaml
api:
  environment:
    # ... existing vars ...
    INTERNAL_PORT: 8443
    ORCHESTRATOR_URL: http://orchestrator:8090
    VK_CLIENT_ID: ${VK_CLIENT_ID:-}
    VK_CLIENT_SECRET: ${VK_CLIENT_SECRET:-}
    VK_REDIRECT_URI: ${VK_REDIRECT_URI:-http://localhost/api/v1/oauth/vk/callback}
    YANDEX_CLIENT_ID: ${YANDEX_CLIENT_ID:-}
    YANDEX_CLIENT_SECRET: ${YANDEX_CLIENT_SECRET:-}
    YANDEX_REDIRECT_URI: ${YANDEX_REDIRECT_URI:-http://localhost/api/v1/oauth/yandex_business/callback}
    TELEGRAM_BOT_TOKEN: ${TELEGRAM_BOT_TOKEN:-}
  ports:
    - "8080:8080"
    - "8443:8443"
  volumes:
    - ./certs/ca.crt:/certs/ca.crt:ro
    - ./certs/server.crt:/certs/server.crt:ro
    - ./certs/server.key:/certs/server.key:ro
```

**Step 3: Remove static business context env vars from orchestrator**

Remove `BUSINESS_NAME`, `BUSINESS_CATEGORY`, `BUSINESS_TONE`, `ACTIVE_INTEGRATIONS` from orchestrator env.

**Step 4: Commit**

```bash
git add docker-compose.yml
git commit -m "feat(docker): add cert volumes, OAuth env vars, remove hardcoded agent tokens"
```

---

## Task 21: Nginx — Route OAuth Callbacks and Chat Proxy

**Files:**
- Modify: `nginx/nginx.conf`

**Step 1: Update nginx config**

Chat now goes through API (not directly to orchestrator). OAuth callbacks go to API.

```nginx
events { worker_connections 1024; }

http {
  upstream api        { server api:8080; }
  upstream frontend   { server frontend:3000; }

  server {
    listen 80;

    # API routes (including OAuth callbacks and chat proxy)
    location /api/v1/ {
      proxy_pass http://api;
      proxy_http_version 1.1;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header Connection '';
      proxy_buffering off;
      proxy_cache off;
    }

    # Frontend
    location / {
      proxy_pass http://frontend;
      proxy_set_header Host $host;
    }
  }
}
```

Key changes:
- Remove `upstream orchestrator` — chat is now proxied through API
- Remove `location /chat/` — no more direct orchestrator access from frontend
- Add SSE-friendly headers to `/api/v1/` (needed for chat proxy streaming)

**Step 2: Commit**

```bash
git add nginx/nginx.conf
git commit -m "feat(nginx): route chat through API proxy, remove direct orchestrator access"
```

---

## Task 22: Frontend — Integrations Page with OAuth Flows

**Files:**
- Modify: `services/frontend/app/(app)/integrations/page.tsx`
- Delete: `services/frontend/components/integrations/ConnectDialog.tsx`
- Modify: `services/frontend/components/integrations/PlatformCard.tsx`
- Create: `services/frontend/components/integrations/TelegramConnectModal.tsx`

**Step 1: Update integrations page**

Replace `ConnectDialog` usage with platform-specific OAuth flows:

For VK and Yandex.Business:
- "Connect" button calls `GET /api/v1/integrations/{platform}/auth-url`
- On success, redirect: `window.location.href = response.data.url`

For Telegram:
- "Connect" button opens `TelegramConnectModal`

Support multi-account display:
- Group integrations by platform
- Show list of connected accounts per platform
- "Add another" button for connecting additional accounts
- Individual disconnect buttons per integration (by integration ID, not platform)

Update the integrations query to handle `Integration[]` with `externalId` field:

```typescript
interface Integration {
  id: string
  platform: string
  status: 'active' | 'inactive' | 'error' | 'pending_cookies' | 'token_expired'
  externalId: string
  metadata?: Record<string, unknown>
  createdAt: string
}
```

**Step 2: Delete ConnectDialog.tsx**

Remove the manual token/cookies entry dialog. It's fully replaced by OAuth flows.

**Step 3: Update PlatformCard**

Update to support multi-account display (show count of connected accounts, individual account rows).

**Step 4: Create TelegramConnectModal**

Three-step wizard:
1. **Step 1**: Instructions to create bot via @BotFather (if not already done) or confirm existing bot
2. **Step 2**: Instructions to add bot to channel as admin
3. **Step 3**: Enter channel username/ID → call `POST /api/v1/integrations/telegram/connect` → verify bot is admin → show success

**Step 5: Commit**

```bash
git add services/frontend/app/(app)/integrations/page.tsx
git add services/frontend/components/integrations/TelegramConnectModal.tsx
git add services/frontend/components/integrations/PlatformCard.tsx
git rm services/frontend/components/integrations/ConnectDialog.tsx
git commit -m "feat(frontend): OAuth-based integration flows, multi-account support"
```

---

## Task 23: Frontend — Update Chat to Use API Proxy

**Files:**
- Modify: `services/frontend/hooks/useChat.ts`

**Step 1: Update fetch URL**

Change from direct orchestrator access:
```typescript
const response = await fetch(`/chat/${conversationId}`, {
```

To API proxy:
```typescript
const response = await fetch(`/api/v1/chat/${conversationId}`, {
```

This is the only change needed — the API proxy handles business context enrichment transparently.

**Step 2: Commit**

```bash
git add services/frontend/hooks/useChat.ts
git commit -m "feat(frontend): route chat through API proxy for business context enrichment"
```

---

## Task Dependency Order

```
Task 1 (Migration) ─────────────────┐
Task 2 (a2a context) ──────────┐    │
                               │    │
Task 3 (Repo methods) ─────────┼────┤
                               │    │
Task 4 (Integration service) ──┤    │
Task 5 (OAuth service) ────────┤    │
Task 6 (API config) ───────────┤    │
                               │    │
Task 7 (Internal token) ───────┤    │
Task 8 (OAuth handlers) ───────┤    │
Task 9 (Chat proxy) ───────────┤    │
                               │    │
Task 10 (Router) ──────────────┤    │
Task 11 (API main wiring) ─────┘    │
                                    │
Task 12 (Token client) ────────────┐│
                                   ││
Task 13 (Orch handler) ───────┐    ││
Task 14 (NATSExecutor) ───────┤    ││
Task 15 (Orch main) ──────────┘    ││
                                   ││
Task 16 (Agent-VK) ────────────────┤│
Task 17 (Agent-Telegram) ──────────┤│
Task 18 (Agent-YB) ────────────────┘│
                                    │
Task 19 (Makefile certs) ──────────┐│
Task 20 (Docker compose) ─────────┤│
Task 21 (Nginx) ───────────────────┘│
                                    │
Task 22 (Frontend integrations) ────┤
Task 23 (Frontend chat) ───────────┘
```

Tasks 1-2 can run in parallel. Tasks 3-6 are sequential. Tasks 7-9 can run in parallel. Tasks 13-15 can run in parallel. Tasks 16-18 can run in parallel. Tasks 19-21 can run in parallel. Tasks 22-23 can run in parallel.

---

## Verification Checklist

After all tasks complete:

1. `go test ./pkg/... ./services/api/... ./services/orchestrator/... -v` — all pass
2. `go build ./services/api/... ./services/orchestrator/... ./services/agent-vk/... ./services/agent-telegram/... ./services/agent-yandex-business/...` — all compile
3. `cd services/frontend && pnpm build` — no TS errors
4. `make certs` — generates certificates in `certs/`
5. `docker-compose config` — valid YAML, no undefined variables
