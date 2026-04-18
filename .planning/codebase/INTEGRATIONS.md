# OneVoice External Integrations

## Database Connections

### PostgreSQL

**Connection Configuration:**
- **File:** `services/api/internal/config/config.go`
- **Driver:** github.com/jackc/pgx/v5 (v5.8.0)
- **Connection Pool:** Built into pgx/v5 (connection pooling enabled by default)
- **DSN Format:** `postgres://user:password@host:port/database?sslmode=disable`
- **Env Variables:**
  - `POSTGRES_HOST` (default: localhost)
  - `POSTGRES_PORT` (default: 5432)
  - `POSTGRES_USER` (default: postgres)
  - `POSTGRES_PASSWORD` (required, no default)
  - `POSTGRES_DB` (default: onevoice)

**Repository Implementations:**
- **Location:** `services/api/internal/repository/postgres_*.go`
- **Query Builder:** Masterminds/squirrel (v1.5.4) for SQL injection prevention
- **Tables:**
  - users, businesses, business_schedules
  - integrations (encrypted OAuth tokens)
  - refresh_tokens (for JWT refresh)
  - subscriptions, audit_logs, integration_events

**Migrations:**
- **Tool:** migrate/migrate:latest
- **Location:** `migrations/postgres/`
- **Execution:** Runs automatically in docker-compose.yml via `migrate` service
- **Command:** `-path /migrations -database "postgres://..." up`

**Connection Pool:**
- Pool size configurable in pgx.Config
- Connection timeout: default 30s
- Health checks: See docker-compose.yml services healthcheck

### MongoDB

**Connection Configuration:**
- **File:** `services/api/internal/config/config.go`
- **Driver:** go.mongodb.org/mongo-driver (v1.17.9 and v2.5.0)
- **URI Format:** `mongodb://[user:password@]host:port[/database]`
- **Env Variables:**
  - `MONGO_URI` (default: mongodb://localhost:27017)
  - `MONGO_DB` (default: onevoice)

**Repository Implementations:**
- **Location:** `services/api/internal/repository/mongo_*.go`
- **Collections:**
  - conversations (stores user-LLM conversations)
  - messages (stores chat history with tool calls/results)

**Collections Schema:**
- Conversations: {_id, businessId, userId, createdAt, updatedAt}
- Messages: {_id, conversationId, role, content, toolCalls[], toolResults[], createdAt}
- Indexes: conversationId, businessId, userId (implicit from queries)

**Initialization:**
- **Location:** `migrations/mongo/`
- **Scripts:** Run on container startup via docker-entrypoint-initdb.d
- **Indexes:** Created in init scripts

**Connection String:**
- Production: Use `mongodb+srv://` for MongoDB Atlas or self-hosted replica sets
- Local dev: `mongodb://localhost:27017`
- Docker Compose: `mongodb://mongodb:27017` (internal network)

### Redis

**Connection Configuration:**
- **File:** `services/api/internal/config/config.go`, `services/orchestrator/internal/config/config.go`
- **Client:** github.com/redis/go-redis/v9 (v9.17.3)
- **Env Variables:**
  - `REDIS_HOST` (default: localhost)
  - `REDIS_PORT` (default: 6379)
  - `REDIS_PASSWORD` (optional, empty by default)

**Use Cases:**
1. **Session Storage:**
   - OAuth state management
   - Temporary user session data
   - Expires automatically per key TTL

2. **Rate Limiting:**
   - User tier-based rate limiting (pkg/llm/ratelimit.go)
   - Token bucket algorithm
   - Key pattern: `rate_limit:{userID}:{tier}`

3. **Caching:**
   - LLM model availability cache
   - Provider health check results
   - Integration token refresh state

**Connection Pool:**
- Client connection pooling: auto-managed by redis client
- Default pool behavior: connections maintained and reused
- Health checks: `redis-cli ping` in docker-compose.yml

**In-Memory Testing:**
- **Library:** github.com/alicebob/miniredis/v2 (v2.36.1)
- **Use:** Unit and integration tests without external Redis
- **Location:** `services/api/internal/repository/*_test.go`

## Message Broker — NATS

**Connection:**
- **Version:** 2.10-alpine with JetStream enabled
- **URL:** `nats://localhost:4222` (local) or `nats://nats:4222` (docker)
- **Env Variable:** `NATS_URL` (optional, empty disables NATS)
- **Go Client:** github.com/nats-io/nats.go (v1.41.1)

**Subject Patterns:**

```
tasks.{agentID}
├── tasks.telegram           (Telegram agent)
├── tasks.vk                 (VK agent)
└── tasks.yandex_business    (Yandex.Business agent)
```

**Message Protocol:**

A2A Framework (pkg/a2a/protocol.go):

**Request (Orchestrator → Agent):**
```json
{
  "task_id": "uuid",
  "tool": "platform__action",
  "args": { "channel_id": "123", "text": "..." },
  "business_id": "uuid",
  "request_id": "uuid" (optional, for tracing)
}
```

**Response (Agent → Orchestrator):**
```json
{
  "task_id": "uuid",
  "success": true/false,
  "result": { "post_id": "456", "url": "..." },
  "error": "error message (if success=false)"
}
```

**Implementation:**
- **Transport Layer:** `pkg/a2a/nats_transport.go`
- **Agent Base:** `pkg/a2a/agent.go` (request/reply pattern)
- **Request/Reply Pattern:** NATS native request-reply with timeout

**Services Using NATS:**
- `services/orchestrator`: Sends tool requests via NATS
- `services/agent-telegram`: Listens on `tasks.telegram`, replies with results
- `services/agent-vk`: Listens on `tasks.vk`, replies with results
- `services/agent-yandex-business`: Listens on `tasks.yandex_business`, replies with results

**Orchestrator Tool Dispatch:**
- **Location:** `services/orchestrator/internal/natsexec/executor.go`
- **Executor:** Implements llm.Executor interface
- **Tool Registry:** `services/orchestrator/internal/tools/registry.go`
- **Tool Naming:** Parses `{platform}__action` to extract platform AgentID

## External APIs

### Telegram Bot API

**Integration:**
- **File:** `services/agent-telegram/internal/telegram/bot.go`
- **SDK:** github.com/go-telegram-bot-api/telegram-bot-api/v5 (v5.5.1)
- **Authentication:** Bot token via `TELEGRAM_BOT_TOKEN` env variable
- **Base URL:** `https://api.telegram.org/bot{token}/`

**Supported Tools:**

**1. telegram__send_channel_post**
- Description: Send a text-only post to a Telegram channel
- Args: {channel_id: string, text: string}
- Returns: {message_id: int, channel_id: int, success: bool}
- API Method: sendMessage

**2. telegram__send_channel_photo**
- Description: Send a photo with optional caption to a Telegram channel
- Args: {channel_id: string, photo_url: string, caption: string}
- Returns: {message_id: int, photo_file_id: string, success: bool}
- API Method: sendPhoto

**3. telegram__get_channel_posts**
- Description: Retrieve recent posts from a Telegram channel
- Args: {channel_id: string, limit: int}
- Returns: {posts: [{message_id, date, text, photo_urls}], total: int}
- API Method: getUpdates (polling approach)

**4. telegram__delete_channel_post**
- Description: Delete a specific post from a Telegram channel
- Args: {channel_id: string, message_id: int}
- Returns: {success: bool}
- API Method: deleteMessage

**5. telegram__get_chat_info**
- Description: Get information about a Telegram chat/channel
- Args: {chat_id: string}
- Returns: {title: string, type: string, member_count: int}
- API Method: getChat

**Implementation:**
- Handler: `services/agent-telegram/internal/agent/handler.go`
- A2A pattern: Receives ToolRequest, dispatches to appropriate method, returns ToolResponse
- Error Handling: Wraps Telegram API errors in ToolResponse.error

**Token Storage:**
- Encrypted in PostgreSQL: `integrations` table
- Column: `encrypted_access_token` (AES-256-GCM encrypted)
- Decryption: `services/api/internal/repository/token_manager.go`

### VK API

**Integration:**
- **File:** `services/agent-vk/internal/vk/client.go`
- **SDK:** github.com/SevereCloud/vksdk/v3 (v3.2.2)
- **Authentication:** OAuth 2.0 access token
- **Base URL:** `https://api.vk.com/method/`
- **API Version:** v5.131 (latest compatible with vksdk/v3)

**OAuth Flow:**
- **Endpoint:** `services/api/internal/handler/oauth.go`
- **Client ID/Secret:** `VK_CLIENT_ID`, `VK_CLIENT_SECRET` env variables
- **Redirect URI:** `VK_REDIRECT_URI` (default: http://localhost/api/v1/oauth/vk/callback)
- **Authorization Endpoint:** `https://oauth.vk.com/authorize`
- **Token Endpoint:** `https://oauth.vk.com/access_token`
- **Scopes:** wall (required for wall posts), groups (for group info)

**Supported Tools:**

**1. vk__create_wall_post**
- Description: Post to a VK user's wall or community wall
- Args: {owner_id: string, message: string, attachments: string}
- Returns: {post_id: int, owner_id: int, success: bool}
- API Method: wall.post

**2. vk__get_wall_posts**
- Description: Retrieve posts from a VK wall
- Args: {owner_id: string, count: int, offset: int}
- Returns: {posts: [{id, text, created_by, likes}], total: int}
- API Method: wall.get

**3. vk__delete_wall_post**
- Description: Delete a post from VK wall
- Args: {owner_id: string, post_id: int}
- Returns: {success: bool}
- API Method: wall.delete

**4. vk__get_community_info**
- Description: Get information about a VK community/group
- Args: {group_id: string}
- Returns: {name: string, description: string, members: int}
- API Method: groups.getById

**Implementation:**
- Handler: `services/agent-vk/internal/agent/handler.go`
- A2A pattern: Same as Telegram agent
- Error Handling: VK API returns error codes; wrapped in ToolResponse.error

**Token Storage:**
- Encrypted in PostgreSQL: `integrations` table
- Refresh Logic: Automatic refresh if `token_expires_at` is near
- File: `services/api/internal/service/oauth.go`

### Yandex.Business API (RPA via Playwright)

**Integration:**
- **File:** `services/agent-yandex-business/internal/yandex/`
- **Approach:** Browser automation (RPA), not direct API
- **Browser:** github.com/playwright-community/playwright-go (v0.4501.1)
- **Authentication:** Session cookies stored as JSON in `YANDEX_COOKIES_JSON` env variable
- **Base URL:** `https://business.yandex.ru/`

**Session Management:**
- Cookies loaded at startup from env variable (JSON array format)
- Cookies persist across page sessions within the agent lifecycle
- Canary check pattern: Verify session validity before executing tasks
- Automatic recovery: Retry with exponential backoff if session expires

**Supported Tools:**

**1. yandex_business__get_reviews**
- Description: Fetch reviews from Yandex.Business dashboard
- Args: {business_id: string, limit: int, offset: int}
- Returns: {reviews: [{id, author, text, rating, date}], total: int}
- Implementation: `services/agent-yandex-business/internal/yandex/get_reviews.go`

**2. yandex_business__reply_review**
- Description: Post a reply to a review on Yandex.Business
- Args: {review_id: string, text: string}
- Returns: {reply_id: string, success: bool}
- Implementation: `services/agent-yandex-business/internal/yandex/reply_review.go`

**3. yandex_business__update_hours**
- Description: Update business hours
- Args: {day: int, open_time: string, close_time: string, is_closed: bool}
- Returns: {success: bool}
- Implementation: `services/agent-yandex-business/internal/yandex/update_hours.go`

**4. yandex_business__update_info**
- Description: Update business information (name, phone, description)
- Args: {field: string, value: string}
- Returns: {success: bool}
- Implementation: `services/agent-yandex-business/internal/yandex/update_info.go`

**RPA Patterns:**
- **withRetry:** Retry with exponential backoff (100ms → 1s → 5s)
- **withPage:** Acquire browser page from pool, execute action, release
- **humanDelay:** Random delay 500-1500ms between actions
- **Canary Check:** Before retry, verify session is still valid
- **Browser Pool:** Reusable browser instance to avoid overhead

**Implementation:**
- Handler: `services/agent-yandex-business/internal/agent/handler.go`
- Browser Manager: `services/agent-yandex-business/internal/yandex/browser.go`
- A2A pattern: Same request/response protocol as other agents

**Error Handling:**
- Network errors: Retried with canary check
- Stale session: Triggers error response (user must refresh cookies)
- Page navigation timeouts: Logged and error returned

## LLM Providers

**Router Location:** `pkg/llm/router.go`, `pkg/llm/provider.go`

**Configuration:**
- **Model Selection:** `LLM_MODEL` env variable (e.g., `openai/gpt-4o-mini`)
- **Parsing:** Format is `provider/model-name`
- **Provider Detection:** Model prefix determines provider

### OpenRouter

**Configuration:**
- **API Key:** `OPENROUTER_API_KEY` env variable
- **Base URL:** `https://openrouter.ai/api/v1/`
- **SDK:** Custom HTTP client (no native Go SDK)
- **Priority:** First in default provider chain

**Supported Models:**
- OpenAI models (via OpenRouter proxy)
- Anthropic models (via OpenRouter proxy)
- Open-source models (Mistral, LLaMA, etc.)
- Custom fine-tuned models

**Model Format:** `openai/gpt-4o`, `anthropic/claude-3-opus`, `mistralai/mistral-7b`, etc.

**Rate Limiting:** Provider-level rate limits enforced by OpenRouter

**Billing Integration:**
- Usage tracked per request (input/output tokens)
- Billing repository logs usage asynchronously
- Commission added per configured tier

### OpenAI

**Configuration:**
- **API Key:** `OPENAI_API_KEY` env variable
- **Base URL:** `https://api.openai.com/v1/`
- **SDK:** github.com/sashabaranov/go-openai (v1.41.2)
- **Priority:** Second in default provider chain

**Supported Models:**
- gpt-4o (latest)
- gpt-4-turbo
- gpt-3.5-turbo

**Model Format:** `openai/gpt-4o`, `openai/gpt-4-turbo`, etc.

**Authentication:** Bearer token in Authorization header

**Rate Limiting:** OpenAI API rate limits per org/user

**Tool Support:** GPT-4 series supports function calling; GPT-3.5 limited support

### Anthropic

**Configuration:**
- **API Key:** `ANTHROPIC_API_KEY` env variable
- **Base URL:** `https://api.anthropic.com/`
- **SDK:** github.com/anthropics/anthropic-sdk-go (v1.22.1)
- **Priority:** Third in default provider chain

**Supported Models:**
- claude-3-opus
- claude-3-sonnet
- claude-3-haiku

**Model Format:** `anthropic/claude-3-opus`, `anthropic/claude-3-sonnet`, etc.

**Authentication:** x-api-key header

**Tool Support:** Tool use (native function calling)

### Self-Hosted (OpenAI-Compatible)

**Configuration:**
- **Multiple Endpoints:** `SELF_HOSTED_N_URL`, `SELF_HOSTED_N_MODEL`, `SELF_HOSTED_N_API_KEY`
- **Index:** N = 0, 1, 2, ... (scanned until first missing URL)
- **Base URL:** Any OpenAI-compatible endpoint (Ollama, vLLM, LocalAI, etc.)
- **Priority:** After commercial providers

**Model Format:** Any string (e.g., `llama-2`, `mistral`, `custom-finetune`)

**Authentication:** Optional API key in Authorization header

**Use Cases:**
- Local inference (Ollama)
- Private data centers (vLLM)
- Custom fine-tuned models

## Authentication Mechanisms

### JWT (JSON Web Tokens)

**Configuration:**
- **File:** `services/api/internal/middleware/auth.go`
- **Library:** github.com/golang-jwt/jwt/v5 (v5.3.1)
- **Algorithm:** HS256 (HMAC with SHA-256)
- **Key:** `JWT_SECRET` env variable (min 32 characters)

**Token Structure:**
```
Header: { alg: "HS256", typ: "JWT" }
Payload: { sub: userID, email: email, role: role, iat: issued_at, exp: expiry }
Signature: HMAC-SHA256(key, header.payload)
```

**Expiration:**
- **Access Token:** 1 hour (configurable in code)
- **Refresh Token:** 7 days (stored in PostgreSQL)

**Token Endpoints:**
- **Login:** POST `/api/v1/auth/login` — returns access + refresh token
- **Refresh:** POST `/api/v1/auth/refresh` — returns new access token
- **Logout:** POST `/api/v1/auth/logout` — invalidates refresh token

**Middleware:**
- **File:** `services/api/internal/middleware/auth.go`
- **Behavior:** Extracts JWT from Authorization header, validates signature and expiry
- **Error:** Returns 401 Unauthorized if invalid or expired

**Refresh Token Storage:**
- **Table:** `refresh_tokens` in PostgreSQL
- **Hash:** Token is hashed before storage (one-way)
- **TTL:** 7 days, auto-expired
- **Revocation:** Soft delete (set expiry to past time)

### OAuth 2.0

**Providers:** VK, Yandex.Business (Telegram uses bot token, no OAuth)

**Authorization Code Flow:**
1. **Authorization Endpoint:**
   - Redirect user to provider (VK/Yandex)
   - Request permissions (scopes)
   - Generate random state (stored in Redis)

2. **Callback:**
   - User redirected back with authorization code
   - State validated against Redis
   - Backend exchanges code for access token

3. **Token Exchange:**
   - `POST {provider_token_endpoint}` with client_id, client_secret, code
   - Receives access token (and refresh token if available)
   - Token immediately encrypted and stored in PostgreSQL

**Handler:** `services/api/internal/handler/oauth.go`

**State Management:**
- **Storage:** Redis (TTL: 10 minutes)
- **Format:** Random 32-byte hex string
- **Validation:** State from callback must match Redis entry

**VK OAuth:**
- **Endpoints:**
  - Auth: `https://oauth.vk.com/authorize`
  - Token: `https://oauth.vk.com/access_token`
- **Scopes:** wall, groups, offline
- **Token Lifetime:** No expiry (lifetime token) or optional 24h refresh

**Yandex.Business OAuth:**
- **Endpoints:**
  - Auth: `https://oauth.yandex.ru/authorize`
  - Token: `https://oauth.yandex.ru/token`
- **Scopes:** business-contact:write, business-contact:read
- **Token Lifetime:** 1 year, requires refresh via refresh token

**Token Refresh:**
- **Automatic:** Checked before each use
- **File:** `services/api/internal/service/oauth.go`
- **Trigger:** If `token_expires_at` is within 1 hour
- **Storage:** New token replaces old, encrypted in PostgreSQL

### AES Encryption for Tokens

**Implementation:** `pkg/crypto/crypto.go`

**Algorithm:** AES-256-GCM

**Key Requirements:**
- Exactly 32 bytes
- Set via `ENCRYPTION_KEY` env variable (validated at startup)
- Never logged or exposed in error messages

**Encryption Process:**
1. Generate random 12-byte nonce
2. Encrypt plaintext (token) with AES-256-GCM
3. Prepend nonce to ciphertext
4. Store as []byte in PostgreSQL (BYTEA column)

**Decryption Process:**
1. Extract nonce (first 12 bytes)
2. Decrypt remaining ciphertext with AES-256-GCM
3. Returns plaintext token

**Use Cases:**
- `encrypted_access_token` column in `integrations` table
- `encrypted_refresh_token` column in `integrations` table

**Decryption Utility:**
- **File:** `services/api/internal/repository/token_manager.go`
- **Function:** `GetDecryptedToken(ctx, businessID, platform, externalID)`
- **Behavior:** Falls back to first active integration if externalID doesn't match

## Webhooks and Callbacks

### OAuth Callbacks

**VK Callback:**
- **Endpoint:** `POST /api/v1/oauth/vk/callback`
- **Parameters:** code (auth code), state (anti-CSRF)
- **Handler:** `services/api/internal/handler/oauth.go`
- **Success Response:** Redirect to frontend with access token in URL
- **Error Response:** Redirect with error message

**Yandex.Business Callback:**
- **Endpoint:** `POST /api/v1/oauth/yandex_business/callback`
- **Parameters:** code, state
- **Handler:** `services/api/internal/handler/oauth.go`
- **Success Response:** Redirect to frontend with access token
- **Error Response:** Redirect with error message

### Telegram Webhooks

**Status:** Not used in current MVP

**Alternative:** Polling via LongPolling API (getUpdates method)
- Implemented in `services/agent-telegram/internal/telegram/polling.go`
- Polling interval: Configurable (default 30s)

### Review Sync Webhook

**Status:** Internal only (no external webhook)

**Trigger:** Scheduled job (NATS-based if enabled)

**Execution:**
- **Service:** `services/api/internal/service/review_sync.go`
- **Interval:** `REVIEW_SYNC_INTERVAL_MINUTES` env variable (default: 30 minutes)
- **Behavior:** Pulls reviews from integrated platforms (async task dispatch via NATS)

## SSE (Server-Sent Events)

**Purpose:** Stream LLM responses and tool calls to frontend in real-time

**Endpoint:** `POST /api/v1/chat/{conversationID}` — returns event stream

**Connection:**
- **Client:** Frontend JavaScript EventSource API
- **Server:** chi HTTP handler with flusher.Flush()
- **Content-Type:** text/event-stream
- **Format:** `data: {json}\n\n`

**Event Types:**
1. **text** — Incremental text response
   - `{"type":"text","content":"hello"}`

2. **tool_call** — LLM requesting tool execution
   - `{"type":"tool_call","tool_name":"telegram__send_post","args":{...}}`

3. **tool_result** — Tool execution result (includes error if failed)
   - `{"type":"tool_result","tool_name":"...", "result":{...}, "error":null}`

4. **done** — Stream complete
   - `{"type":"done"}`

5. **error** — Stream error (premature termination)
   - `{"type":"error","error":"message"}`

**Handler:** `services/api/internal/handler/chat.go`

**Implementation:**
- Calls orchestrator SSE endpoint: `GET /chat/{conversationID}`
- Proxies events to frontend with potential transformations
- Accumulates tool calls/results and saves to MongoDB on completion

## Rate Limiting

**Implementation:** `pkg/llm/ratelimit.go`

**Storage:** Redis

**Algorithm:** Token bucket

**Configuration:**
- **Free Tier:** 100 requests/hour
- **Pro Tier:** 1000 requests/hour
- **Enterprise:** Unlimited (or custom)

**Key Pattern:** `rate_limit:{userID}:{tier}`

**Enforcement:**
- Checked before LLM call in router
- Returns `ErrRateLimitExceeded` if exceeded
- Error propagated to frontend

## Billing

**Implementation:** `pkg/llm/billing.go`

**Tracking:**
- **Unit:** Tokens (input and output separately)
- **Per Request:** Logged after LLM response received
- **Repository:** `domain.BillingRepository` interface

**Storage:** PostgreSQL table (TBD structure)

**Async Logging:**
- Billing logged in background goroutine
- Does not block response to user
- Pattern: `go r.logBilling(context.Background(), ...)`

**Commission Model:**
- **Percentage Mode:** Fixed markup (e.g., 15%)
- **Flat Fee Mode:** Per-request fee
- **Tiered Mode:** Different markup per tier (free, pro, enterprise)

**Configuration:**
- **File:** `pkg/llm/config.go` (CommissionConfig type)
- **YAML:** Provider/pricing config file (location TBD)
- **Env Override:** Per-model pricing overrides possible

## Troubleshooting / Integration Patterns

### Token Refresh Fallback

**File:** `services/api/internal/repository/token_manager.go`

**Pattern:** Two-step lookup for decryption
1. If `externalID` provided → try exact match in `integrations` table
2. If not found → fall back to first active integration for that platform

**Reason:** Handles LLM hallucination (e.g., wrong channel_id) gracefully

### Telegram Channel ID Resolution

**File:** `services/agent-telegram/internal/agent/handler.go`

**Pattern:**
- LLM may pass channel_id as numeric string, business name, or empty
- If strconv.ParseInt fails → use `resolvedExternalID` from integration
- Fallback ensures LLM errors don't 404

### Yandex.Business Canary Check

**File:** `services/agent-yandex-business/internal/yandex/browser.go`

**Pattern:**
1. Execute action
2. If error or stale session detected → perform canary check (e.g., verify login)
3. If canary fails → return error (cookies expired)
4. If canary passes → retry action with exponential backoff

### Tool Call Persistence

**File:** `services/api/internal/handler/chat.go` (chat_proxy pattern)

**Pattern:**
1. Stream SSE events during LLM execution
2. Accumulate `tool_call` and `tool_result` events in memory
3. On stream completion (done event) → save tool calls/results to MongoDB
4. On frontend load → retrieve from MongoDB and re-render tool call panel

**Data Structure:** `domain.ToolCall[]` and `domain.ToolResult[]` stored on Message document
