# OneVoice Architecture

Platform-agnostic multi-agent system for automating digital presence management across social platforms. Built with Go 1.24 microservices, Next.js 14 frontend, PostgreSQL, MongoDB, NATS messaging, and Playwright RPA.

## System Overview

OneVoice is a **microservices architecture** coordinated via Go workspace (`go.work`), with clear service boundaries communicating through REST APIs, SSE (Server-Sent Events), and NATS message queue.

### High-Level Data Flow

```
┌─────────────┐
│  Frontend   │ (Next.js 14, port 3000)
│ (port 3000) │
└──────┬──────┘
       │ REST API (auth, business, integrations, conversations)
       │ SSE (chat stream)
       ↓
┌─────────────────┐
│   API Service   │ (port 8080)
│  PostgreSQL     │
│  MongoDB        │
│  Redis          │
└────────┬────────┘
         │ HTTP request → /chat/{id}
         ↓
┌──────────────────────┐
│   Orchestrator       │ (port 8090)
│  (LLM agent loop)    │
└────────┬─────────────┘
         │ NATS request/reply
         │ tasks.{agentID}
         ├──────────────────┬──────────────────┬──────────────────┐
         ↓                  ↓                  ↓                  ↓
    ┌─────────┐        ┌─────────┐        ┌─────────┐        (more platforms)
    │Telegram │        │   VK    │        │ Yandex  │
    │ Agent   │        │ Agent   │        │Business │
    │(NATS)   │        │(NATS)   │        │(NATS)   │
    └─────────┘        └─────────┘        └─────────┘
         │                  │                   │
         ↓                  ↓                   ↓
    Telegram Bot API   VK API            Yandex.Business
                                         (RPA via Playwright)
```

## Service Boundaries

OneVoice consists of 6 deployable services:

### 1. **API Service** (`services/api/`)
**Port:** 8080

Central REST API handling user authentication, business management, integrations, and conversation persistence.

**Responsibilities:**
- User registration, login, JWT token generation
- Business profile CRUD operations
- Platform integration management (OAuth tokens for VK, Yandex; Telegram bot tokens)
- Conversation and message persistence (MongoDB)
- Review/post/task data management
- Token encryption/decryption for platform credentials
- Rate limiting (Redis-backed)
- Internal token endpoint for agents to fetch credentials

**Key Dependencies:**
- PostgreSQL (users, businesses, integrations)
- MongoDB (conversations, messages, reviews, posts, tasks)
- Redis (sessions, rate limiting)
- JWT (auth tokens)
- Encryption (platform credential storage)

**Communication:**
- **Inbound:** REST endpoints from frontend, agents
- **Outbound:** HTTP calls to orchestrator (/chat), internal port 8443 for agent token requests

---

### 2. **Orchestrator Service** (`services/orchestrator/`)
**Port:** 8090

LLM-powered agent orchestration engine. Receives chat requests, runs an agentic loop (LLM → tool → NATS → agent result → repeat), and streams responses via SSE.

**Responsibilities:**
- LLM provider integration (OpenRouter, OpenAI, Anthropic, Self-hosted)
- Multi-iteration agent loop with tool dispatch
- Tool registry and NATS request/reply coordination
- Business context and system prompt generation
- Rate limiting and billing aggregation
- SSE streaming to frontend

**Key Abstractions:**
- `llm.Router`: LLM provider abstraction (tries providers in order)
- `llm.Registry`: Available providers registry with credentials
- `tools.Registry`: Tool definitions mapped to platform agents
- `orchestrator.Orchestrator`: Agent loop state machine
- `natsexec.NATSExecutor`: Tool execution via NATS

**Communication:**
- **Inbound:** POST /chat/{conversationID} with business context and chat history
- **Outbound:** NATS request/reply on subjects `tasks.{agentID}` (e.g., `tasks.telegram`)

---

### 3. **Telegram Agent** (`services/agent-telegram/`)

NATS-based agent handling Telegram Bot API operations.

**Responsibilities:**
- Subscribe to NATS subject `tasks.telegram`
- Parse incoming A2A TaskRequest with tool name and arguments
- Call Telegram Bot API (via `go-telegram-bot-api/v5`)
- Return A2A TaskResponse with result or error
- Token management via API's internal endpoint

**Tool Implementations:**
- `telegram__send_channel_post` — text message to channel
- `telegram__send_channel_photo` — image + caption to channel
- `telegram__send_notification` — DM to business owner

**Communication:**
- **Inbound:** NATS subject `tasks.telegram`
- **Outbound:** HTTP requests to Telegram Bot API, token fetches to API service

---

### 4. **VK Agent** (`services/agent-vk/`)

NATS-based agent for VK API operations.

**Responsibilities:**
- Subscribe to NATS subject `tasks.vk`
- Parse incoming tool calls
- Call VK API (VKontakte)
- Return results/errors via NATS reply

**Tool Implementations:**
- `vk__publish_post` — community post
- `vk__get_posts` — fetch community posts
- Other VK-specific operations

**Communication:**
- **Inbound:** NATS subject `tasks.vk`
- **Outbound:** HTTP to VK API, token fetches to API service

---

### 5. **Yandex.Business Agent** (`services/agent-yandex-business/`)

RPA-based agent for Yandex.Business operations via Playwright browser automation.

**Responsibilities:**
- Subscribe to NATS subject `tasks.yandex_business`
- Automate Yandex.Business UI with Playwright
- Handle review publishing, scheduling
- Return operation results via NATS reply

**Tool Implementations:**
- `yandex_business__publish_review` — post business review
- `yandex_business__schedule_post` — schedule a post
- Other Yandex-specific operations

**Communication:**
- **Inbound:** NATS subject `tasks.yandex_business`
- **Outbound:** Browser automation to Yandex.Business, token fetches to API service

---

### 6. **Frontend Service** (`services/frontend/`)
**Port:** 3000

Next.js 14 dashboard for end-users.

**Responsibilities:**
- User authentication UI (login, register)
- Business profile management
- Integration setup (OAuth flows, token management)
- Chat UI with LLM agent interaction
- Message history and tool call visualization
- Review, post, and task management

**Communication:**
- **Inbound:** Browser requests
- **Outbound:** REST API to API service (port 8080), SSE stream from orchestrator (port 8090)

---

## Shared Libraries (`pkg/`)

### `pkg/domain/`
**Core domain models and repository interfaces.**

**Files:**
- `models.go` — PostgreSQL models (User, Business, Integration, etc.)
- `mongo_models.go` — MongoDB models (Conversation, Message, Review, Post, Task)
- `repository.go` — Repository interfaces (UserRepository, BusinessRepository, etc.)
- `errors.go` — Domain error types
- `roles.go` — User role constants

**Usage:** All services reference these types for consistency.

---

### `pkg/a2a/` (Agent-to-Agent Communication)
**Protocol and framework for inter-agent messaging.**

**Files:**
- `protocol.go` — A2A TaskRequest/TaskResponse types, AgentID constants
- `agent.go` — Agent base class (subscription, request handling)
- `nats_transport.go` — NATS-based transport implementation
- `context.go` — Context utilities

**Key Types:**
- `AgentID`: enum (AgentTelegram="telegram", AgentVK="vk", AgentYandexBusiness="yandex_business")
- `TaskRequest`: {tool_name, arguments}
- `TaskResponse`: {result, error}

**Usage:** All platform agents instantiate `a2a.Agent` with NATS transport.

---

### `pkg/llm/` (LLM Integration)
**Multi-provider LLM router with provider adapters, rate limiting, and billing.**

**Submodules:**

#### `pkg/llm/types.go`
- `Message` — user/assistant/system messages
- `ToolDefinition` — tool schema (name, description, parameters)
- `Choice` — LLM response choice
- `Provider` interface — provider abstraction

#### `pkg/llm/router.go`
- `Router` — multi-provider router (tries each provider in order)
- Handles provider fallback on rate limits/errors

#### `pkg/llm/providers/`
- `openrouter.go` — OpenRouter API integration
- `openai.go` — OpenAI API integration
- `anthropic.go` — Anthropic Claude API integration
- `selfhosted.go` — Self-hosted LLM (OpenAI-compatible)

**Rate Limiting:**
- `ratelimit.go` — Per-provider token bucket
- Prevents exceeding provider limits

**Billing:**
- `billing.go` — Token usage tracking (input/output tokens)
- Async fire-and-forget logging to API

**Usage:** Orchestrator wires Router with providers based on env vars.

---

### `pkg/crypto/`
**Encryption/decryption for platform credentials.**

**Files:**
- `crypto.go` — AES encryption wrapper

**Usage:** API service encrypts/decrypts platform OAuth tokens before storage.

---

### `pkg/tokenclient/`
**Client for API's token endpoint.**

**Files:**
- `client.go` — HTTP client calling API's `/internal/tokens/{businessID}` endpoint

**Usage:** Platform agents use this to fetch decrypted tokens at runtime.

---

### `pkg/logger/`
**Structured logging utility.**

**Files:**
- `logger.go` — slog-based logger factory

**Usage:** All services initialize logger with `logger.New("service-name")`.

---

## Key Abstractions & Interfaces

### LLM Provider Interface (`pkg/llm/provider.go`)
```go
type Provider interface {
    Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error)
}
```
Each provider (OpenRouter, OpenAI, Anthropic, Self-hosted) implements this.

### Repository Pattern (`pkg/domain/repository.go`)
```go
type UserRepository interface {
    GetByID(ctx context.Context, id string) (*User, error)
    Create(ctx context.Context, user *User) error
    // ...
}
```
All data access through repositories; no direct DB queries in handlers/services.

### A2A Agent Base (`pkg/a2a/agent.go`)
```go
type Agent struct {
    id        AgentID
    transport Transport
    handler   Handler
}

func (a *Agent) Start(ctx context.Context) error
```
Platform agents embed this to subscribe to NATS and handle requests.

### Tool Registry (`services/orchestrator/internal/tools/registry.go`)
```go
type Registry struct {
    tools map[string]Executor
}

func (r *Registry) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)
```
Maps tool names to executors (NATS-based for platform agents, internal for system tools).

---

## Communication Patterns

### 1. REST (API ↔ Frontend)
**Synchronous request/response.**

**Endpoints (sample):**
- `POST /auth/register` — user registration
- `POST /auth/login` — JWT generation
- `GET /businesses/{id}` — business profile
- `POST /conversations/{id}/messages` — new message
- `POST /chat/{conversationID}` — chat request to orchestrator
- `GET /integrations` — list active integrations
- `POST /oauth/{platform}/callback` — OAuth redirect handler

**Error Handling:** HTTP status codes + JSON error responses.

---

### 2. SSE (Orchestrator → Frontend)
**Server-sent events for streaming agent loop.**

**Flow:**
1. Frontend: `POST /chat/{conversationID}` with business context, messages
2. Orchestrator: Accepts request, begins agent loop
3. Orchestrator: Streams `data: {"type":"text","content":"..."}\n\n` for each event type
4. Event types: `text`, `tool_call`, `tool_result`, `done`, `error`
5. Frontend: Consumes stream, updates UI incrementally

**Tool Call Event Example:**
```json
{"type":"tool_call","tool_name":"telegram__send_channel_post","arguments":{"text":"Hello"}}
```

**Tool Result Event Example:**
```json
{"type":"tool_result","tool_name":"telegram__send_channel_post","tool_result":{"message_id":123},"tool_error":""}
```

---

### 3. NATS Request/Reply (Orchestrator ↔ Agents)
**Asynchronous distributed messaging.**

**Pattern:**
1. Orchestrator: Identifies tool as `{platform}__{action}` (e.g., `telegram__send_channel_post`)
2. Orchestrator: Creates A2A TaskRequest → JSON marshal → NATS request to `tasks.{platform}`
3. Agent (e.g., Telegram): Listens on `tasks.telegram`, unmarshals TaskRequest
4. Agent: Executes operation (calls Telegram API), marshals TaskResponse
5. Agent: Replies to NATS request inbox
6. Orchestrator: Receives reply, unmarshals TaskResponse, continues agent loop

**Request Timeout:** 30 seconds (configurable).

---

### 4. Internal HTTP (Agents → API)
**Token and credential fetching.**

**Endpoint:**
- `GET /internal/tokens/{businessID}?platform=telegram&externalID={channelID}`

**Response:**
```json
{"access_token":"...", "external_id":"..."}
```

**Usage:** Agents call this before API operations to get decrypted credentials.

---

## Deployment Model

### Go Workspace (`go.work`)
**Single workspace with 6 modules:**
- `./pkg` — shared libraries
- `./services/api`
- `./services/orchestrator`
- `./services/agent-telegram`
- `./services/agent-vk`
- `./services/agent-yandex-business`
- `./test/integration`

**Each service has `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in its `go.mod`.**

### Docker Compose
**`docker-compose.yml` orchestrates all infrastructure:**
- PostgreSQL (port 5432)
- MongoDB (port 27017)
- Redis (port 6379)
- NATS (port 4222)
- Database migrations
- All 6 services

### Environment Variables
**Centralized in docker-compose, can be overridden per service:**
- Database credentials
- JWT secret
- Encryption key
- OAuth secrets (VK, Yandex)
- Telegram bot token
- LLM API keys (OpenRouter, OpenAI, Anthropic)
- Service URLs (orchestrator, API)

---

## Data Flow: Complete Chat Request

### Step 1: Frontend Initiates Chat
```
POST /chat/{conversationID}
{
  "messages": [...],
  "activeIntegrations": ["telegram", "vk"],
  "businessContext": {
    "name": "My Business",
    "description": "...",
    "schedule": {...}
  }
}
```

### Step 2: API Proxies to Orchestrator
```
POST http://orchestrator:8090/chat/{conversationID}
(forward body)
Accept: text/event-stream
```

### Step 3: Orchestrator Begins Agent Loop
- Builds system prompt with business context
- Calls LLM (via Router) with history + tools
- LLM returns: `{text, tool_calls: [...]}`

### Step 4: For Each Tool Call
1. Extract tool name (e.g., `telegram__send_channel_post`)
2. Extract platform (`telegram`)
3. Create NATS TaskRequest
4. Publish to `tasks.telegram` with request/reply pattern
5. Wait for agent response (30s timeout)

### Step 5: Agent Processes Tool
1. Telegram Agent receives request
2. Fetches token: `GET /internal/tokens/{businessID}?platform=telegram&externalID=...`
3. Calls Telegram API
4. Returns TaskResponse with result/error

### Step 6: Orchestrator Feeds Result Back to LLM
- LLM continues with tool result in context
- May make more tool calls or generate final response
- Max 10 iterations guard

### Step 7: Stream Response to Frontend
- Each LLM response chunk: `data: {...}\n\n`
- Tool calls and results: `data: {"type":"tool_result",...}\n\n`
- Final: `data: {"type":"done"}\n\n`

### Step 8: Frontend & API Persist
- Frontend displays streamed response
- API's `chat_proxy.go` handler accumulates tool calls/results
- Saves to MongoDB as Message document

---

## Key Design Decisions

### 1. Microservices via Go Workspace
- Enables independent deployment while sharing code
- Each service is a separate binary with its own `go.mod`
- Simplifies testing and module dependency management

### 2. NATS for Agent Coordination
- Decouples orchestrator from agents
- Enables dynamic agent registration
- Supports request/reply pattern for tool execution
- Scalable: agents can be replicated

### 3. Dual Database
- **PostgreSQL** for transactional data (users, businesses, integrations)
- **MongoDB** for flexible documents (conversations, messages, reviews)
- Separation reduces schema coupling

### 4. SSE for Chat Streaming
- Server pushes events to client as they occur
- Lower latency than polling
- Works over HTTP (no WebSocket overhead)

### 5. Multi-Provider LLM Router
- No vendor lock-in
- Fallback on provider failure/rate limits
- Consistent interface across providers

### 6. A2A Protocol Abstraction
- Platform agents are interchangeable
- New agent can be added without modifying orchestrator
- Request/reply enables bidirectional debugging

### 7. Playwright for RPA
- Handles dynamic UIs (Yandex.Business)
- Captures screenshots for debugging
- Retries with backoff for flaky operations

---

## Extension Points

### Adding a New Platform Agent
1. Create `services/agent-{platform}/`
2. Implement `internal/agent/handler.go` (A2A Handler interface)
3. Create platform client in `internal/{platform}/`
4. Register tool definitions in orchestrator's `cmd/main.go`
5. Update agent startup: `a2a.NewAgent(a2a.AgentID("{platform}"), ...)`

### Adding a New Tool
1. Define tool in orchestrator's `tools/registry.go`
2. Implement executor (either NATS-based or inline)
3. Update LLM provider's tool list
4. Implement handler in corresponding agent

### Adding a New LLM Provider
1. Create `pkg/llm/providers/{provider}.go`
2. Implement `Provider` interface
3. Add provider initialization to orchestrator's `buildProviderOpts()`
4. Set env var for API key

### Adding a New Repository Type
1. Define model in `pkg/domain/models.go` or `mongo_models.go`
2. Define repository interface in `pkg/domain/repository.go`
3. Implement repository in `services/api/internal/repository/{entity}.go`
4. Wire into API's `cmd/main.go`
5. Create handler endpoints in `services/api/internal/handler/{entity}.go`

---

## Error Handling Strategy

### Domain Errors (`pkg/domain/errors.go`)
- `ErrNotFound` — entity doesn't exist
- `ErrUnauthorized` — auth failure
- `ErrRateLimited` — rate limit exceeded
- `ErrConflict` — business logic conflict
- Handlers convert domain errors to HTTP status codes

### Tool Execution Errors
- Tool returns `{error: "reason"}` in TaskResponse
- Orchestrator includes error in LLM context ("tool failed because...")
- LLM can retry or report to user

### Provider Errors
- Provider returns error → Router tries next provider
- After all providers fail → stream error event to frontend
- User sees: "All LLM providers unavailable"

---

## Concurrency & Goroutines

### API Service
- HTTP handlers spawned per request (chi handles pooling)
- Billing logged async: `go r.logBilling(context.Background(), ...)`
- Repository pooling: pgx maintains connection pool

### Orchestrator
- Agent loop runs in request handler goroutine
- Each tool call: NATS request with timeout context
- Rate limiter: token bucket (no goroutines, atomic operations)

### Agents
- Single goroutine per agent (NATS subscription is blocking)
- Token fetches: concurrent HTTP calls (no pooling needed)
- Playwright operations: sequential per page (browser state)

---

## Security Considerations

### Authentication
- JWT tokens stored in secure cookies (frontend)
- Token refresh handled by middleware
- Internal endpoints protected by separate API key (agents)

### Token Encryption
- Platform OAuth tokens encrypted with AES-GCM
- Encryption key from `ENCRYPTION_KEY` env var
- Agents never see plaintext tokens (fetched at runtime)

### Rate Limiting
- Redis-backed token bucket per IP
- Prevents brute force attacks
- API and platform rate limits coordinated

### CORS
- Frontend restricted to API domain
- NATS not exposed to internet
- All inter-service communication: internal network

### Input Validation
- All HTTP inputs validated against schemas
- NATS messages validated after unmarshaling
- LLM tool arguments validated before execution

---

## Observability

### Logging
- Structured logging with slog (JSON format in production)
- Each service logs start/stop, errors, tool execution
- Correlation IDs for request tracing

### Metrics
- Billing tracks LLM tokens by provider
- Tool execution times recorded
- Database query performance monitored (via pgx)

### Error Tracking
- Errors logged with stack traces
- User-facing errors differentiated from system errors
- NATS timeouts logged for agent health

---

## Scalability Considerations

### Horizontal Scaling
- **API:** Stateless, run multiple instances behind load balancer
- **Orchestrator:** Stateless, run multiple instances (NATS ensures one response per request)
- **Agents:** Stateless, run multiple replicas per platform (NATS load-balances)

### Vertical Scaling
- **Database:** PostgreSQL/MongoDB can be scaled independently
- **Cache:** Redis can be tuned for rate limiting
- **NATS:** Cluster mode for message durability

### Bottlenecks
- LLM latency: mitigated by provider fallback + rate limiting
- Database queries: optimized with indexes, connection pooling
- NATS message size: limited by conservative tool definitions

