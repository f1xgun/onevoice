# OneVoice Directory Structure

Comprehensive guide to the codebase layout, module organization, and key file locations.

## Top-Level Directory Layout

```
/Users/f1xgun/onevoice/
├── go.work                          # Go workspace declaration (6 modules)
├── go.work.sum                      # Go workspace checksum
├── CLAUDE.md                        # Project instructions (developer reference)
├── CODING_RULES.md                  # Detailed coding standards
├── Makefile                         # Build, test, lint targets
├── docker-compose.yml               # Full local environment orchestration
├── Dockerfile                       # API service container
├── Dockerfile.orchestrator          # Orchestrator service container
├── Dockerfile.agent-telegram        # Telegram agent container
├── Dockerfile.agent-vk              # VK agent container
├── Dockerfile.agent-yandex-business # Yandex.Business agent container
├── .env                             # Environment variables (local development)
├── .env.example                     # Template for .env
├── .editorconfig                    # Editor formatting rules
├── .golangci.yml                    # golangci-lint configuration
├── .gitignore                       # Git ignore rules
├── lefthook.yml                     # Git hooks (pre-commit, pre-push)
├── LICENSE                          # MIT license
├── SETUP_INSTRUCTIONS.md            # Local development setup guide
│
├── docs/                            # Documentation
│   ├── architecture.md              # System architecture overview
│   ├── golden-principles.md         # Design principles
│   ├── patterns.md                  # Code patterns (used throughout)
│   ├── anti-patterns.md             # Anti-patterns to avoid
│   ├── TECHNICAL_REQUIREMENTS.md    # Original requirements document
│   ├── VKR_ROADMAP.md               # Long-term roadmap
│   └── plans/                       # Phase implementation plans (historical)
│       ├── 2026-02-09-phase1-backend-*.md
│       ├── 2026-02-12-phase2-llm-*.md
│       ├── 2026-02-14-phase3-a2a-*.md
│       ├── 2026-02-14-phase4-platform-*.md
│       ├── 2026-02-16-frontend-*.md
│       └── 2026-02-23-e2e-*.md
│
├── pkg/                             # Shared Go libraries (Go module: github.com/f1xgun/onevoice/pkg)
│   ├── go.mod                       # Shared library dependencies
│   ├── go.sum                       # Dependency checksums
│   ├── CLAUDE.md                    # Package-level documentation
│   │
│   ├── domain/                      # Core domain models & repository interfaces
│   │   ├── models.go                # PostgreSQL models: User, Business, Integration
│   │   ├── mongo_models.go          # MongoDB models: Conversation, Message, Review, Post, Task
│   │   ├── repository.go            # Repository interfaces (all entity types)
│   │   ├── errors.go                # Domain error types
│   │   └── roles.go                 # Role constants (user, admin)
│   │
│   ├── a2a/                         # Agent-to-Agent Communication Framework
│   │   ├── protocol.go              # TaskRequest, TaskResponse, AgentID enum
│   │   ├── protocol_test.go
│   │   ├── agent.go                 # Agent base class (subscribe, handle, reply)
│   │   ├── agent_test.go
│   │   ├── nats_transport.go        # NATS-based transport implementation
│   │   ├── nats_transport_test.go
│   │   ├── context.go               # Context utilities
│   │   └── context_test.go
│   │
│   ├── llm/                         # LLM Integration & Multi-Provider Router
│   │   ├── types.go                 # Message, ToolDefinition, Choice, Response types
│   │   ├── types_test.go
│   │   ├── provider.go              # Provider interface abstraction
│   │   ├── provider_test.go
│   │   ├── router.go                # Multi-provider router (fallback logic)
│   │   ├── router_test.go
│   │   ├── registry.go              # Provider registry (credentials lookup)
│   │   ├── registry_test.go
│   │   ├── config.go                # Config types for provider initialization
│   │   ├── config_test.go
│   │   ├── ratelimit.go             # Per-provider token bucket rate limiter
│   │   ├── ratelimit_test.go
│   │   ├── billing.go               # Token usage tracking & cost calculation
│   │   ├── billing_test.go
│   │   ├── integration_test.go      # End-to-end integration tests
│   │   │
│   │   └── providers/               # LLM Provider Implementations
│   │       ├── openrouter.go        # OpenRouter API client
│   │       ├── openrouter_test.go
│   │       ├── openai.go            # OpenAI API client
│   │       ├── openai_test.go
│   │       ├── anthropic.go         # Anthropic Claude API client
│   │       ├── anthropic_test.go
│   │       ├── selfhosted.go        # Self-hosted LLM (OpenAI-compatible)
│   │       ├── selfhosted_test.go
│   │       └── compliance_test.go   # Cross-provider compliance tests
│   │
│   ├── crypto/                      # Encryption/Decryption for Token Storage
│   │   ├── crypto.go                # AES-GCM encryptor
│   │   └── crypto_test.go
│   │
│   ├── tokenclient/                 # Client for API's Token Endpoint
│   │   ├── client.go                # HTTP client to /internal/tokens
│   │   └── client_test.go
│   │
│   └── logger/                      # Structured Logging Utility
│       └── logger.go                # slog-based logger factory
│
├── services/                        # Microservices (6 deployable services)
│   │
│   ├── api/                         # REST API Service (Port: 8080)
│   │   ├── go.mod                   # Service dependencies
│   │   ├── go.sum
│   │   ├── CLAUDE.md                # Service documentation
│   │   │
│   │   ├── cmd/
│   │   │   └── main.go              # Entry point: DB setup, dependency injection, router wiring
│   │   │
│   │   └── internal/                # Private implementation
│   │       ├── config/
│   │       │   └── config.go        # Configuration loading (env vars to Config struct)
│   │       │
│   │       ├── handler/             # HTTP Request Handlers (Chi routes)
│   │       │   ├── auth.go          # POST /register, /login, /logout, /refresh
│   │       │   ├── business.go      # GET/POST /businesses, CRUD operations
│   │       │   ├── integration.go   # GET/POST /integrations, platform credential management
│   │       │   ├── conversation.go  # GET /conversations, conversation listing
│   │       │   ├── chat_proxy.go    # POST /chat (proxy to orchestrator, stream handling)
│   │       │   ├── post.go          # GET/POST /posts (post management)
│   │       │   ├── review.go        # GET/POST /reviews (review management)
│   │       │   ├── agent_task.go    # Internal endpoint: /agent_tasks
│   │       │   ├── oauth.go         # OAuth callback handlers (/oauth/{platform}/callback)
│   │       │   ├── internal_token.go # Internal endpoint: /internal/tokens (agents fetch credentials)
│   │       │   ├── response.go      # Response formatting utilities
│   │       │   └── *_test.go        # Handler tests
│   │       │
│   │       ├── middleware/          # HTTP Middleware
│   │       │   ├── auth.go          # JWT verification, token refresh
│   │       │   ├── cors.go          # CORS headers
│   │       │   ├── logging.go       # Request/response logging
│   │       │   ├── ratelimit.go     # Redis-backed rate limiting
│   │       │   └── *_test.go        # Middleware tests
│   │       │
│   │       ├── service/             # Business Logic Layer
│   │       │   ├── user.go          # User registration, password hashing
│   │       │   ├── business.go      # Business profile CRUD
│   │       │   ├── integration.go   # OAuth token management, encryption
│   │       │   ├── oauth.go         # OAuth flow orchestration (VK, Yandex)
│   │       │   ├── post.go          # Post creation, retrieval
│   │       │   ├── review.go        # Review management
│   │       │   ├── review_sync.go   # Review sync from platforms (background job)
│   │       │   ├── agent_task.go    # Agent task management
│   │       │   └── *_test.go        # Service tests
│   │       │
│   │       ├── repository/          # Data Access Layer
│   │       │   ├── user.go          # PostgreSQL: User CRUD
│   │       │   ├── business.go      # PostgreSQL: Business CRUD
│   │       │   ├── integration.go   # PostgreSQL: Integration CRUD (OAuth tokens)
│   │       │   ├── conversation.go  # MongoDB: Conversation queries
│   │       │   ├── message.go       # MongoDB: Message queries
│   │       │   ├── post.go          # MongoDB: Post queries
│   │       │   ├── review.go        # MongoDB: Review queries
│   │       │   ├── agent_task.go    # MongoDB: Agent task queries
│   │       │   ├── pool.go          # Database connection pool utilities
│   │       │   └── *_test.go        # Repository tests
│   │       │
│   │       ├── router/              # HTTP Router Configuration
│   │       │   └── router.go        # Chi router setup, route definitions
│   │       │
│   │       └── platform/            # Platform-Specific Logic
│   │           └── (platform integrations: VK, Yandex OAuth details)
│   │
│   ├── orchestrator/                # LLM Orchestrator Service (Port: 8090)
│   │   ├── go.mod                   # Service dependencies (chi, nats.go, LLM SDKs)
│   │   ├── go.sum
│   │   ├── CLAUDE.md                # Service documentation
│   │   │
│   │   ├── cmd/
│   │   │   └── main.go              # Entry: LLM provider setup, tool registry, NATS wiring
│   │   │
│   │   └── internal/                # Private implementation
│   │       ├── config/
│   │       │   ├── config.go        # Configuration (LLM_MODEL required, PORT=8090, MAX_ITERATIONS, NATS_URL)
│   │       │   └── config_test.go
│   │       │
│   │       ├── handler/
│   │       │   ├── chat.go          # POST /chat/{conversationID} SSE handler
│   │       │   └── chat_test.go
│   │       │
│   │       ├── orchestrator/        # Agent Loop State Machine
│   │       │   ├── orchestrator.go  # Run() method: LLM call → tool dispatch → repeat
│   │       │   └── orchestrator_test.go
│   │       │
│   │       ├── tools/               # Tool Registry
│   │       │   ├── registry.go      # Tool definitions, executor mapping
│   │       │   └── registry_test.go
│   │       │
│   │       ├── prompt/              # Prompt Engineering
│   │       │   ├── builder.go       # Build() system prompt + history + tools
│   │       │   └── builder_test.go
│   │       │
│   │       └── natsexec/            # NATS Tool Executor
│   │           ├── executor.go      # NATS request/reply for tool execution
│   │           ├── executor_test.go
│   │           └── nats_conn.go     # NATS connection adapter
│   │
│   ├── agent-telegram/              # Telegram Bot API Agent
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── CLAUDE.md
│   │   │
│   │   ├── cmd/
│   │   │   └── main.go              # Entry: NATS connection, token client, agent initialization
│   │   │
│   │   └── internal/
│   │       ├── agent/
│   │       │   └── handler.go       # A2A Handler: dispatches tool calls to Telegram client
│   │       │
│   │       └── telegram/
│   │           └── bot.go           # Telegram Bot API client wrapper
│   │
│   ├── agent-vk/                    # VK API Agent
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── CLAUDE.md
│   │   │
│   │   ├── cmd/
│   │   │   └── main.go              # Entry: NATS connection, token client, agent initialization
│   │   │
│   │   └── internal/
│   │       ├── agent/
│   │       │   └── handler.go       # A2A Handler: VK API operations
│   │       │
│   │       └── vk/
│   │           └── client.go        # VK API client wrapper
│   │
│   ├── agent-yandex-business/       # Yandex.Business RPA Agent (Playwright)
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── CLAUDE.md
│   │   │
│   │   ├── cmd/
│   │   │   └── main.go              # Entry: NATS connection, Playwright setup, agent initialization
│   │   │
│   │   └── internal/
│   │       ├── agent/
│   │       │   └── handler.go       # A2A Handler: RPA operations
│   │       │
│   │       └── yandex/
│   │           ├── browser.go       # Playwright browser automation
│   │           ├── auth.go          # Login/authentication flow
│   │           └── operations.go    # Review publish, post scheduling, etc.
│   │
│   └── frontend/                    # Next.js 14 Dashboard (Port: 3000)
│       ├── package.json             # Next.js dependencies, scripts
│       ├── tsconfig.json            # TypeScript configuration
│       ├── tailwind.config.ts       # Tailwind CSS configuration
│       ├── next.config.js           # Next.js configuration
│       ├── .env.local               # Local environment (API_URL, ORCHESTRATOR_URL)
│       ├── jest.config.js           # Jest testing configuration
│       │
│       ├── app/                     # Next.js App Router
│       │   ├── layout.tsx           # Root layout (providers, fonts, globals)
│       │   ├── page.tsx             # Landing page (/)
│       │   ├── globals.css          # Global styles
│       │   ├── favicon.ico
│       │   ├── fonts/               # Custom fonts
│       │   │
│       │   ├── (public)/            # Public routes (no auth required)
│       │   │   ├── login/
│       │   │   │   └── page.tsx     # Login UI
│       │   │   └── register/
│       │   │       └── page.tsx     # Registration UI
│       │   │
│       │   └── (app)/               # Private routes (auth required)
│       │       ├── layout.tsx       # App layout (sidebar, header)
│       │       ├── chat/
│       │       │   ├── page.tsx     # Conversation list
│       │       │   └── [id]/
│       │       │       └── page.tsx # Chat view with message history & LLM interaction
│       │       ├── business/
│       │       │   └── page.tsx     # Business profile form (schedule, description)
│       │       ├── integrations/
│       │       │   └── page.tsx     # Integration setup (OAuth flows, token management)
│       │       ├── posts/
│       │       │   └── page.tsx     # Post list and management
│       │       ├── reviews/
│       │       │   └── page.tsx     # Review list and management
│       │       ├── tasks/
│       │       │   └── page.tsx     # Task list and management
│       │       └── settings/
│       │           └── page.tsx     # User settings
│       │
│       ├── components/              # Reusable UI Components
│       │   ├── sidebar.tsx          # Navigation sidebar
│       │   ├── providers.tsx        # Context providers (auth, theme, etc.)
│       │   │
│       │   ├── ui/                  # Shadcn/ui Components (third-party library)
│       │   │   ├── button.tsx
│       │   │   ├── input.tsx
│       │   │   ├── dialog.tsx
│       │   │   ├── form.tsx
│       │   │   ├── card.tsx
│       │   │   ├── table.tsx
│       │   │   ├── tabs.tsx
│       │   │   ├── dropdown-menu.tsx
│       │   │   └── (20+ other components)
│       │   │
│       │   ├── chat/                # Chat-Specific Components
│       │   │   ├── ChatWindow.tsx   # Main chat UI
│       │   │   ├── MessageBubble.tsx # Message display
│       │   │   ├── ToolCallsBlock.tsx # Tool execution visualization
│       │   │   └── ToolCard.tsx     # Individual tool card
│       │   │
│       │   ├── business/            # Business Profile Components
│       │   │   ├── ProfileForm.tsx  # Business info form
│       │   │   └── ScheduleForm.tsx # Schedule management
│       │   │
│       │   └── integrations/        # Integration Components
│       │       ├── ConnectDialog.tsx # Integration connection dialog
│       │       ├── PlatformCard.tsx # Platform card UI
│       │       ├── PlatformIcons.tsx # Platform SVG icons
│       │       └── TelegramConnectModal.tsx # Telegram token entry
│       │
│       ├── hooks/                   # Custom React Hooks
│       │   ├── useChat.ts           # Chat state management (SSE streaming, messages)
│       │   ├── use-toast.ts         # Toast notification hook
│       │   └── __tests__/           # Hook tests
│       │
│       ├── lib/                     # Utility Functions
│       │   ├── api.ts               # API client (fetch wrapper)
│       │   ├── auth.ts              # Auth utilities (token management)
│       │   ├── platforms.ts         # Platform constants (Telegram, VK, Yandex)
│       │   ├── schemas.ts           # Zod validation schemas
│       │   ├── utils.ts             # General utilities
│       │   └── __tests__/           # Lib tests
│       │
│       ├── types/                   # TypeScript Type Definitions
│       │   ├── business.ts          # Business type definitions
│       │   ├── chat.ts              # Chat/message types
│       │   ├── review.ts            # Review types
│       │   ├── post.ts              # Post types
│       │   └── task.ts              # Task types
│       │
│       ├── public/                  # Static assets
│       │   └── (images, logos, etc.)
│       │
│       └── node_modules/            # npm dependencies (generated)
│
├── migrations/                      # Database Migrations
│   ├── postgres/                    # PostgreSQL migrations (flyway/migrate format)
│   │   ├── 000001_init.up.sql       # Initial schema (users, businesses, integrations)
│   │   ├── 000001_init.down.sql
│   │   ├── 000002_multi_account_integrations.up.sql # Multi-account integration support
│   │   └── 000002_multi_account_integrations.down.sql
│   │
│   └── mongo/                       # MongoDB initialization
│       └── init.js                  # Create indexes for conversations, messages, reviews
│
├── test/                            # Testing Directory
│   ├── integration/                 # Integration tests
│   │   ├── go.mod                   # Test module dependencies
│   │   └── (e2e test suites)
│   │
│   └── e2e/                         # End-to-end tests (Playwright-based UI tests)
│       ├── package.json
│       ├── playwright.config.ts
│       └── tests/
│           ├── auth.spec.ts         # Login/register tests
│           ├── chat.spec.ts         # Chat interaction tests
│           └── (other e2e tests)
│
├── bin/                             # Build Artifacts & Scripts
│   ├── api                          # Compiled API binary
│   ├── orchestrator                 # Compiled orchestrator binary
│   ├── agent-telegram               # Compiled Telegram agent binary
│   ├── agent-vk                     # Compiled VK agent binary
│   └── agent-yandex-business        # Compiled Yandex agent binary
│
├── certs/                           # SSL Certificates (local development)
│   ├── localhost.crt
│   └── localhost.key
│
├── nginx/                           # Nginx Reverse Proxy Configuration
│   ├── nginx.conf                   # Main config (routes to services)
│   └── Dockerfile.nginx             # Nginx container
│
├── prd/                             # Product Requirements (non-code docs)
│   └── (PRD documents)
│
├── vkr/                             # VKR (Thesis/Coursework) Documentation
│   ├── latex-template/              # LaTeX template
│   ├── latex-report/                # LaTeX report
│   ├── latex-specs/                 # Specifications in LaTeX
│   ├── tasks/                       # Task management docs
│   ├── plans/                       # Planning docs
│   ├── prompts/                     # LLM prompts (documentation)
│   └── (other thesis-related files)
│
└── .claude/                         # Claude Code Agent Configuration
    ├── agents/
    │   └── ui-reviewer.md           # UI Review agent instructions
    ├── commands/                    # Custom commands
    │   └── gsd/                     # GSD (Get Shit Done) command
    ├── hooks/                       # Git hooks integration
    └── worktrees/                   # Git worktree management
```

---

## Service Structure Patterns

### Standard Service Layout

All 6 services follow this pattern:

```
services/{service-name}/
├── go.mod                    # Module definition
├── go.sum                    # Dependency checksums
├── CLAUDE.md                 # Service-specific documentation
│
├── cmd/
│   └── main.go               # Entry point (wiring, initialization)
│
├── internal/
│   ├── config/               # Configuration loading
│   │   └── config.go
│   │
│   ├── handler/              # Request handlers (HTTP for API, A2A for agents)
│   │   └── {entity}.go
│   │
│   ├── service/              # Business logic (API only)
│   │   └── {entity}.go
│   │
│   ├── repository/           # Data access (API only)
│   │   └── {entity}.go
│   │
│   └── {platform}/           # Platform-specific client
│       └── {client}.go
│
└── migrations/               # Database migrations (API only)
    └── {version}_{name}.{up|down}.sql
```

### Layering Rules

**API Service:** Handler → Service → Repository (3-layer cake)
- Handlers: Parse HTTP, call service, format response
- Services: Business logic, validation, error wrapping
- Repositories: SQL/Mongo queries only

**Agents:** Handler (A2A) → Platform Client
- Handler: Receives A2A TaskRequest, dispatches to platform
- Platform Client: API calls/RPA operations

**Orchestrator:** Handler (HTTP) → Orchestrator → Tools
- Handler: Receives chat request, calls orchestrator
- Orchestrator: Agent loop state machine
- Tools: Tool execution (NATS or internal)

---

## Database Schema Locations

### PostgreSQL Migrations
**Path:** `migrations/postgres/`

**Versions:**
- `000001_init.up.sql` — Users, businesses, integrations schema
- `000002_multi_account_integrations.up.sql` — Multi-account support

**Schema Overview:**
```sql
users              -- user accounts
├── id, email, password_hash, created_at
businesses         -- business profiles
├── id, user_id, name, description, schedule, created_at
integrations       -- platform OAuth tokens
├── id, business_id, platform, access_token (encrypted), external_id
oauth_tokens       -- OAuth state for flows
└── id, business_id, platform, state, created_at
```

### MongoDB Collections
**Path:** `migrations/mongo/init.js`

**Collections:**
- `conversations` — Chat sessions
- `messages` — Chat messages (with tool calls/results)
- `reviews` — Platform reviews
- `posts` — Published posts
- `tasks` — Background tasks
- `agent_tasks` — Agent-specific tasks

---

## Configuration File Locations

### Environment Configuration
- **Docker Compose:** `docker-compose.yml` (all services, databases)
- **Local Dev:** `.env` and `.env.example`
- **Service Configs:**
  - API: `services/api/internal/config/config.go`
  - Orchestrator: `services/orchestrator/internal/config/config.go`
  - Frontend: `services/frontend/.env.local`

### Build Configuration
- **Go Workspace:** `go.work`
- **Frontend:** `tsconfig.json`, `tailwind.config.ts`, `next.config.js`
- **Linting:** `.golangci.yml` (all Go modules)
- **Git Hooks:** `lefthook.yml`
- **Docker:** Dockerfile per service + docker-compose.yml

---

## Frontend Structure Details

### Pages (App Router)

#### Public Pages
- `app/(public)/login/page.tsx` — User login
- `app/(public)/register/page.tsx` — User registration

#### Authenticated Pages
- `app/(app)/chat/page.tsx` — Conversation list
- `app/(app)/chat/[id]/page.tsx` — Individual chat (streaming)
- `app/(app)/business/page.tsx` — Business profile form
- `app/(app)/integrations/page.tsx` — Integration management
- `app/(app)/posts/page.tsx` — Post list
- `app/(app)/reviews/page.tsx` — Review list
- `app/(app)/tasks/page.tsx` — Task list
- `app/(app)/settings/page.tsx` — User settings

### Component Organization

#### Domain Components
- `components/chat/` — Chat UI (ChatWindow, MessageBubble, ToolCalls)
- `components/business/` — Business profile (ProfileForm, ScheduleForm)
- `components/integrations/` — Integration setup (ConnectDialog, PlatformCard)

#### UI Components
- `components/ui/` — Shadcn/ui library (40+ reusable components)
  - Form elements (input, select, textarea, etc.)
  - Containers (card, dialog, sheet, etc.)
  - Data display (table, tabs, badge, etc.)

### Hooks

#### useChat
**Location:** `hooks/useChat.ts`

State management for chat:
- Message history
- SSE stream handling
- Tool call execution
- Error handling

**Usage:**
```typescript
const { messages, streamMessage, sendMessage, toolCalls } = useChat(conversationID);
```

#### use-toast
**Location:** `hooks/use-toast.ts`

Toast notification system for user feedback.

### Type Definitions

**Location:** `types/`

- `business.ts` — Business, Schedule types
- `chat.ts` — Conversation, Message, ToolCall types
- `review.ts` — Review types
- `post.ts` — Post types
- `task.ts` — Task types

### API Client

**Location:** `lib/api.ts`

Fetch wrapper with:
- Authorization headers
- Error handling
- JSON serialization
- Response formatting

**Example:**
```typescript
const response = await api.post(`/conversations/${id}/messages`, { content: "Hello" });
```

---

## Go Package Organization

### Module Dependencies

Each service has `replace` directive in `go.mod`:
```
replace github.com/f1xgun/onevoice/pkg => ../../pkg
```

### Import Paths

**Shared library:**
```go
import "github.com/f1xgun/onevoice/pkg/domain"
import "github.com/f1xgun/onevoice/pkg/llm"
import "github.com/f1xgun/onevoice/pkg/a2a"
```

**Service internals:**
```go
import "github.com/f1xgun/onevoice/services/api/internal/handler"
import "github.com/f1xgun/onevoice/services/api/internal/service"
import "github.com/f1xgun/onevoice/services/api/internal/repository"
```

### File Naming Conventions

- **Models:** `{entity}.go` (e.g., `user.go`, `business.go`)
- **Tests:** `{file}_test.go` (e.g., `user_test.go`)
- **Interfaces:** Same file as implementation or `repository.go`
- **Handlers:** `{entity}.go` (e.g., `auth.go`, `business.go`)
- **Services:** `{entity}.go` (e.g., `user.go`, `oauth.go`)
- **Repositories:** `{entity}.go` (e.g., `user.go`, `conversation.go`)
- **Middleware:** `{name}.go` (e.g., `auth.go`, `ratelimit.go`)
- **Config:** `config.go` (single file per service)

---

## Test File Organization

### Unit Tests
**Location:** Alongside source files with `_test.go` suffix

**Examples:**
- `pkg/domain/errors_test.go` — Domain errors
- `pkg/llm/router_test.go` — LLM router logic
- `services/api/internal/handler/auth_test.go` — Auth handler tests

### Integration Tests
**Location:** `test/integration/`

End-to-end tests:
- Database connection tests
- API endpoint tests
- LLM router fallback tests
- A2A protocol tests

### E2E Tests
**Location:** `test/e2e/`

Playwright-based UI tests:
- `tests/auth.spec.ts` — Login/register flow
- `tests/chat.spec.ts` — Chat interaction
- Headless browser automation

---

## Build Artifacts

### Compiled Binaries
**Location:** `bin/`

Generated by `make build`:
- `bin/api` — API service binary
- `bin/orchestrator` — Orchestrator service binary
- `bin/agent-telegram` — Telegram agent binary
- `bin/agent-vk` — VK agent binary
- `bin/agent-yandex-business` — Yandex agent binary

### Frontend Build Output
**Location:** `services/frontend/.next/`

Generated by `npm run build`:
- Static assets
- Server-side rendered routes
- Optimized JavaScript

---

## Documentation Locations

### Architecture & Design
- `docs/architecture.md` — System overview
- `docs/golden-principles.md` — Design principles
- `docs/patterns.md` — Code patterns
- `docs/anti-patterns.md` — Anti-patterns

### Implementation Plans
- `docs/plans/` — Phase implementation plans (historical reference)

### Service Documentation
- `services/api/CLAUDE.md` — API service guide
- `services/orchestrator/CLAUDE.md` — Orchestrator service guide
- `services/agent-telegram/CLAUDE.md` — Telegram agent guide
- `pkg/CLAUDE.md` — Shared library guide

### Root Documentation
- `CLAUDE.md` — Project overview (entry point)
- `CODING_RULES.md` — Detailed coding standards
- `SETUP_INSTRUCTIONS.md` — Local development setup
- `README.md` (if exists) — Project overview

---

## Key File Listing

### API Service Files
**Configuration:**
- `services/api/cmd/main.go` — Entry point
- `services/api/internal/config/config.go` — Config loading

**HTTP Layer:**
- `services/api/internal/handler/auth.go` — Auth endpoints
- `services/api/internal/handler/business.go` — Business endpoints
- `services/api/internal/handler/integration.go` — Integration endpoints
- `services/api/internal/handler/chat_proxy.go` — Orchestrator proxy
- `services/api/internal/handler/internal_token.go` — Agent token endpoint
- `services/api/internal/router/router.go` — Route setup

**Business Logic:**
- `services/api/internal/service/user.go` — User operations
- `services/api/internal/service/business.go` — Business operations
- `services/api/internal/service/integration.go` — Integration management
- `services/api/internal/service/oauth.go` — OAuth flows
- `services/api/internal/service/review_sync.go` — Review background sync

**Data Access:**
- `services/api/internal/repository/user.go` — User repo
- `services/api/internal/repository/business.go` — Business repo
- `services/api/internal/repository/integration.go` — Integration repo
- `services/api/internal/repository/conversation.go` — Conversation repo (MongoDB)
- `services/api/internal/repository/message.go` — Message repo (MongoDB)

### Orchestrator Service Files
**Configuration:**
- `services/orchestrator/cmd/main.go` — Entry point, provider setup
- `services/orchestrator/internal/config/config.go` — Config loading

**HTTP Layer:**
- `services/orchestrator/internal/handler/chat.go` — SSE chat handler

**Agent Loop:**
- `services/orchestrator/internal/orchestrator/orchestrator.go` — Agent loop
- `services/orchestrator/internal/prompt/builder.go` — Prompt engineering
- `services/orchestrator/internal/tools/registry.go` — Tool registry
- `services/orchestrator/internal/natsexec/executor.go` — NATS executor

### Shared Library Files
**Domain:**
- `pkg/domain/models.go` — PostgreSQL models
- `pkg/domain/mongo_models.go` — MongoDB models
- `pkg/domain/repository.go` — Repository interfaces

**LLM:**
- `pkg/llm/types.go` — Message types
- `pkg/llm/router.go` — Provider router
- `pkg/llm/providers/openai.go` — OpenAI adapter
- `pkg/llm/providers/openrouter.go` — OpenRouter adapter
- `pkg/llm/providers/anthropic.go` — Anthropic adapter

**A2A:**
- `pkg/a2a/protocol.go` — TaskRequest/TaskResponse
- `pkg/a2a/agent.go` — Agent base class
- `pkg/a2a/nats_transport.go` — NATS transport

### Frontend Files
**Entry Points:**
- `services/frontend/app/layout.tsx` — Root layout
- `services/frontend/app/page.tsx` — Landing page
- `services/frontend/app/(app)/layout.tsx` — App layout

**Chat:**
- `services/frontend/app/(app)/chat/[id]/page.tsx` — Chat page
- `services/frontend/components/chat/ChatWindow.tsx` — Chat UI
- `services/frontend/hooks/useChat.ts` — Chat state

**API Integration:**
- `services/frontend/lib/api.ts` — API client
- `services/frontend/lib/auth.ts` — Auth utilities

---

## Code File Statistics

### API Service
- **Handlers:** 19 files (~600 lines)
- **Services:** 12 files (~400 lines)
- **Repositories:** 13 files (~500 lines)
- **Middleware:** 5 files (~250 lines)
- **Total:** ~1750 lines of business logic

### Orchestrator Service
- **Handler:** 1 file (~100 lines)
- **Orchestrator:** 1 file (~200 lines)
- **Tools/Prompt:** 2 files (~300 lines)
- **NATS Executor:** 2 files (~150 lines)
- **Total:** ~750 lines of orchestration logic

### Shared Libraries
- **Domain:** 5 files (~300 lines)
- **LLM:** 20 files (~2000 lines)
- **A2A:** 6 files (~300 lines)
- **Crypto/Logger/TokenClient:** 3 files (~200 lines)
- **Total:** ~2800 lines of shared code

### Frontend
- **App Pages:** 13 files (~500 lines)
- **Components:** 40+ files (~1500 lines)
- **Hooks:** 2 files (~200 lines)
- **Lib/Types:** 10 files (~300 lines)
- **Total:** ~2500 lines of TypeScript/React

---

## Import Path Patterns

### Cross-Module Imports

**From Service to Shared:**
```go
import (
    "github.com/f1xgun/onevoice/pkg/domain"
    "github.com/f1xgun/onevoice/pkg/llm"
    "github.com/f1xgun/onevoice/pkg/a2a"
)
```

**Within Service (Private):**
```go
import (
    "github.com/f1xgun/onevoice/services/api/internal/handler"
    "github.com/f1xgun/onevoice/services/api/internal/service"
    "github.com/f1xgun/onevoice/services/api/internal/repository"
)
```

**From Frontend to Library:**
```typescript
import { api } from "@/lib/api";
import { useChat } from "@/hooks/useChat";
import { Business } from "@/types/business";
```

---

## Makefile Targets

**Location:** `Makefile`

**Build:**
- `make build` — Compile all Go services
- `make build-docker` — Build Docker images

**Testing:**
- `make test-all` — Run all Go tests
- `make test-api` — API tests only
- `make test-integration` — Integration tests

**Linting:**
- `make lint-all` — golangci-lint all modules
- `make fmt-fix` — Auto-format all code

**Development:**
- `make run-compose` — Start docker-compose environment
- `make migrate` — Run database migrations

---

## Performance Optimization Hints

### Database Query Locations
- PostgreSQL queries: `services/api/internal/repository/{entity}.go`
- MongoDB queries: `services/api/internal/repository/{entity}.go`
- Use `pgx` connection pool for PostgreSQL
- Use MongoDB driver's connection pooling

### Hot Path Code
- `pkg/llm/router.go` — LLM provider selection (every chat request)
- `services/orchestrator/internal/orchestrator/orchestrator.go` — Agent loop (every iteration)
- `services/api/internal/handler/chat_proxy.go` — Chat streaming (every message)

### Caching Opportunities
- Integration tokens (fetch once, cache in memory)
- Tool definitions (static, load once)
- Provider rate limits (per-provider tracking)

---

## Critical Paths for Debugging

### Chat Request Flow
1. Frontend: `services/frontend/app/(app)/chat/[id]/page.tsx` (SSE listener)
2. API: `services/api/internal/handler/chat_proxy.go` (proxy handler)
3. Orchestrator: `services/orchestrator/internal/handler/chat.go` (SSE handler)
4. Orchestrator: `services/orchestrator/internal/orchestrator/orchestrator.go` (agent loop)
5. NATS: `services/orchestrator/internal/natsexec/executor.go` (tool dispatch)
6. Agent: `services/agent-{platform}/internal/agent/handler.go` (tool handler)

### Authentication Flow
1. Frontend: `services/frontend/app/(public)/login/page.tsx` (login form)
2. API: `services/api/internal/handler/auth.go` (POST /login)
3. API: `services/api/internal/service/user.go` (password verification)
4. API: Returns JWT token
5. Frontend: Stores token in secure cookie
6. API: `services/api/internal/middleware/auth.go` (JWT verification)

### Integration Setup Flow
1. Frontend: `services/frontend/app/(app)/integrations/page.tsx` (OAuth init)
2. API: `services/api/internal/handler/oauth.go` (OAuth redirect)
3. API: `services/api/internal/service/oauth.go` (token exchange)
4. API: `services/api/internal/repository/integration.go` (token storage, encrypted)
5. Database: PostgreSQL `integrations` table

