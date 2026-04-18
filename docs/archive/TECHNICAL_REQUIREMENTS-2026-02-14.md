# OneVoice — Technical Requirements for Implementation

> **Purpose**: This document is a self-contained reference for an AI agent orchestrating the implementation of OneVoice. It consolidates all architectural decisions, technical specifications, constraints, and phasing from the PRD, thesis documentation, and approved decisions.
>
> **Last updated**: 2026-02-14
>
> **Status**: Backend core (Phase 1), LLM Router (Phase 2.1–2.4), Orchestrator (Phase 2.5), and A2A framework (Phase 3) are implemented. See §15 for detailed phase breakdown.

---

## Table of Contents

1. [Project Summary](#1-project-summary)
2. [Approved Decisions](#2-approved-decisions)
3. [Tech Stack](#3-tech-stack)
4. [Repository Structure](#4-repository-structure)
5. [Architecture Overview](#5-architecture-overview)
6. [Agent Specifications](#6-agent-specifications)
7. [LLM Orchestrator](#7-llm-orchestrator)
8. [Platform Integrations](#8-platform-integrations)
9. [Database Schema](#9-database-schema)
10. [REST API Specification](#10-rest-api-specification)
11. [Frontend Requirements](#11-frontend-requirements)
12. [Infrastructure & Deployment](#12-infrastructure--deployment)
13. [Security Requirements](#13-security-requirements)
14. [Non-Functional Requirements](#14-non-functional-requirements)
15. [Implementation Phases](#15-implementation-phases)
16. [Risks & Constraints](#16-risks--constraints)
17. [Thesis Deliverable Minimums](#17-thesis-deliverable-minimums)

---

## 1. Project Summary

**OneVoice** is a platform-agnostic multi-agent system with a **hybrid integration model (API + RPA)** for automating digital presence management for small and medium businesses (SMBs).

### Core Idea

An LLM orchestrator coordinates specialized agents — each responsible for a specific external platform. The user interacts via natural language (Russian). The system maintains a "single source of truth" for business data and synchronizes it across all connected platforms.

### Key Innovation

**Hybrid integration**: Some platforms (VK, Telegram) have official APIs; others (Yandex.Business, 2GIS — the dominant map services in Russia) have **no public API at all**. The system uses browser automation (Playwright/RPA) for the latter. The orchestrator doesn't know or care how an agent interacts with its platform — the interface is uniform.

### Target Market

- Russia as the primary case study (VK, Telegram, Yandex.Business)
- Architecture is universal — swap agents for any country's platforms without changing the core

### Domain Example

A coffee shop ("кофейня") is used throughout as the reference scenario.

---

## 2. Approved Decisions

These decisions are final and **must not be changed** without explicit user approval.

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | **Domain example**: coffee shop (кофейня) | Most relatable for SMB thesis scenario |
| 2 | **Tech stack**: Go + Next.js/React + PostgreSQL + MongoDB | Confirmed by user; PG for relational, MongoDB for flexible schema (see §3, §9) |
| 3 | **Competitors analyzed**: Bitrix24, SendPulse, Kommo, Hootsuite, Buffer, SMMplanner, YouScan | Covers CRM/SMM/ORM classes |
| 4 | **Hybrid integration model**: API agents (VK, Telegram) + RPA agents (Yandex.Business via Playwright) in MVP | Yandex Maps 53%, 2GIS 44%, Google Maps 3% in Russia |
| 5 | **Instagram removed** from project scope entirely | Meta recognized as extremist organization in Russia; legally blocked; advertising prohibited since Sep 2025 |
| 6 | **Google Business Profile deprioritized** to future | Only 3% usage in Russia; relevant for international markets only |
| 7 | **MVP platforms**: VK (API) + Telegram (API) + Yandex.Business (RPA) | Covers social + messaging + maps for Russia |
| 8 | **Evaluation approach**: Compare API vs RPA agents on **test instances** of real platforms (not real businesses) | Metrics: execution time, reliability, resilience to UI changes |
| 9 | **Agent protocol**: A2A over NATS request-reply (not MCP) | Custom `pkg/a2a` with ToolRequest/ToolResponse; simpler than MCP, better fit for Go |
| 10 | **Multi-provider LLM routing**: OpenRouter (primary) + direct OpenAI + direct Anthropic | Strategy-based selection (cost/speed), rate limiting, billing with commission |
| 11 | **Одноклассники excluded** from target platforms | Narrower scope for thesis MVP feasibility |

---

## 3. Tech Stack

### Backend

| Component | Technology | Version | Notes |
|-----------|-----------|---------|-------|
| Language | **Go** | 1.24+ | All backend services, agents, orchestrator |
| HTTP Router | **go-chi/chi** | v5 | Lightweight, middleware-friendly |
| Message Queue | **NATS** | Latest stable | Inter-service communication via request-reply |
| Agent Protocol | **A2A** (Agent-to-Agent) | Custom over NATS | `pkg/a2a` — ToolRequest/ToolResponse types |
| RPA Engine | **Playwright** | Via `playwright-go` | Browser automation for platforms without APIs |
| LLM Router | **Multi-provider** | See §7 | OpenRouter (primary), OpenAI, Anthropic — strategy-based routing |
| SQL Builder | **squirrel** | Latest | Type-safe SQL query building with `pgx` |
| Validation | **go-playground/validator** | v10 | Request input validation |
| JWT | **golang-jwt/jwt** | v5 | Stateless JWT access tokens |
| Password Hashing | **bcrypt** | `golang.org/x/crypto` | Secure password storage |

### Frontend

| Component | Technology | Version | Notes |
|-----------|-----------|---------|-------|
| Framework | **Next.js** | 14 | SSR, routing, API routes |
| UI Library | **React** | 18 | Component model |
| Styling | **Tailwind CSS** + **shadcn/ui** | Latest | Utility-first + pre-built components |

### Data Layer

| Component | Technology | Version | Notes |
|-----------|-----------|---------|-------|
| Primary DB | **PostgreSQL** | 16 | Relational data: users, businesses, integrations, subscriptions |
| Document DB | **MongoDB** | 7 | Flexible-schema data: conversations, messages, tasks, reviews, posts |
| Cache / Sessions | **Redis** | 7 | Refresh tokens, rate limiting, pub/sub |
| File Storage | **MinIO** | Latest | S3-compatible, self-hosted |

### Go Dependencies (Key Packages)

| Package | Purpose |
|---------|---------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver + connection pool |
| `github.com/Masterminds/squirrel` | SQL query builder |
| `go.mongodb.org/mongo-driver/v2` | MongoDB driver |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/golang-jwt/jwt/v5` | JWT generation/validation |
| `github.com/go-playground/validator/v10` | Struct validation |
| `github.com/google/uuid` | UUID generation |
| `github.com/sashabaranov/go-openai` | OpenAI + OpenRouter SDK |
| `github.com/anthropics/anthropic-sdk-go` | Anthropic SDK |
| `github.com/nats-io/nats.go` | NATS client |
| `github.com/stretchr/testify` | Test assertions |
| `gopkg.in/yaml.v3` | YAML config parsing |

### Infrastructure

| Component | Technology | Notes |
|-----------|-----------|-------|
| Local Dev | **Docker Compose** | All services containerized |
| Production | **Kubernetes** | Horizontal scaling |
| CI/CD | **GitHub Actions** | Lint, test, build, deploy |
| Monitoring | **Prometheus + Grafana** | Metrics + dashboards |
| Logging | **Loki** + structured JSON | Integrated with Grafana |
| Tracing | **OpenTelemetry** | Distributed tracing |

---

## 4. Repository Structure

The project uses a **Go multi-module workspace** (`go.work`) with separate Go modules for shared packages, each service, and integration tests. This enables independent dependency management and build isolation.

```
onevoice/
├── go.work                     # Go workspace: pkg, services/api, services/orchestrator, test/integration
│
├── pkg/                        # Shared packages (module: github.com/f1xgun/onevoice/pkg)
│   ├── go.mod
│   ├── domain/                 # Domain models, errors, repository interfaces
│   │   ├── models.go           # PostgreSQL entities (User, Business, Integration, etc.)
│   │   ├── mongo_models.go     # MongoDB entities (Conversation, Message, AgentTask, Review, Post)
│   │   ├── roles.go            # Role enum (owner, admin, member)
│   │   ├── errors.go           # Sentinel errors (ErrUserNotFound, ErrBusinessNotFound, etc.)
│   │   └── repository.go       # Repository interfaces (UserRepository, BusinessRepository, etc.)
│   ├── llm/                    # LLM Router + provider abstraction
│   │   ├── provider.go         # Provider interface (Chat, ChatStream, ListModels, HealthCheck)
│   │   ├── types.go            # ChatRequest, ChatResponse, Message, ToolCall, StreamChunk, ModelInfo
│   │   ├── config.go           # Config types for llm.yaml (providers, commission, model filter)
│   │   ├── registry.go         # Model registry: model → provider mapping + metrics
│   │   ├── ratelimit.go        # Redis-based rate limiter (per-user, per-tier limits)
│   │   ├── billing.go          # UsageLog, BillingRepository interface, commission calculation
│   │   ├── router.go           # Router: strategy-based provider selection, billing, rate limiting
│   │   └── providers/          # Concrete provider adapters
│   │       ├── openrouter.go   # OpenRouter (primary — proxies OpenAI/Anthropic models)
│   │       ├── openai.go       # Direct OpenAI
│   │       └── anthropic.go    # Direct Anthropic
│   ├── a2a/                    # Agent-to-Agent protocol
│   │   ├── protocol.go         # ToolRequest, ToolResponse, AgentID constants, Subject()
│   │   └── agent.go            # Agent base: Transport interface, Handler, Subscribe + dispatch
│   ├── crypto/                 # AES-256-GCM encryption
│   │   └── crypto.go
│   └── logger/                 # Structured logging (slog + JSON)
│       └── logger.go
│
├── services/
│   ├── api/                    # API Gateway + Core API (module: .../services/api)
│   │   ├── go.mod
│   │   ├── cmd/
│   │   │   └── main.go         # Wiring: DB connections → repos → services → handlers → chi router
│   │   └── internal/
│   │       ├── repository/     # PostgreSQL repos (squirrel + pgx) + MongoDB repos
│   │       │   ├── user.go
│   │       │   ├── business.go
│   │       │   ├── integration.go
│   │       │   └── conversation.go  # MongoDB
│   │       ├── service/        # Business logic (auth, business, integrations)
│   │       │   ├── user.go     # Register, Login, Refresh, Logout
│   │       │   ├── business.go # Get, Update, UpdateSchedule
│   │       │   └── integration.go
│   │       ├── handler/        # HTTP handlers (chi)
│   │       │   ├── auth.go     # 5 endpoints
│   │       │   ├── business.go # 4 endpoints
│   │       │   ├── integration.go # 3 endpoints
│   │       │   └── conversation.go # 3 endpoints
│   │       └── middleware/     # JWT auth, CORS, rate limiting, request logging
│   │
│   └── orchestrator/           # LLM Orchestrator service (module: .../services/orchestrator)
│       ├── go.mod
│       ├── cmd/
│       │   └── main.go         # Wiring: config → LLM Router → tools → orchestrator → chi → SSE
│       └── internal/
│           ├── config/         # Env var config (PORT, LLM_MODEL, NATS_URL, etc.)
│           ├── prompt/         # System prompt builder (BusinessContext → []llm.Message)
│           ├── tools/          # Tool registry with integration-based filtering
│           ├── orchestrator/   # Agent loop: build messages → LLM → tool calls → loop → text response
│           ├── handler/        # SSE chat handler (POST /chat/{conversationID})
│           └── natsexec/       # NATS executor: tools.Executor → A2A ToolRequest over NATS
│
├── migrations/
│   ├── postgres/
│   │   ├── 000001_init.up.sql    # 8 tables + triggers + indexes
│   │   └── 000001_init.down.sql
│   └── mongo/
│       └── init.js               # MongoDB indexes for 5 collections
│
├── web/                        # Next.js frontend (future)
│   ├── src/
│   │   ├── app/                # App Router pages
│   │   ├── components/         # React components
│   │   ├── lib/                # Utilities, API client
│   │   └── hooks/              # Custom React hooks
│   ├── package.json
│   └── next.config.js
│
├── test/
│   └── integration/            # Integration tests (module: .../test/integration)
│       └── go.mod
│
├── deployments/
│   ├── docker/
│   │   ├── docker-compose.yml
│   │   ├── Dockerfile.api
│   │   ├── Dockerfile.orchestrator
│   │   ├── Dockerfile.agent-vk
│   │   ├── Dockerfile.agent-telegram
│   │   └── Dockerfile.agent-yandex-business
│   └── kubernetes/
│
├── docs/                       # Documentation + implementation plans
│   └── plans/                  # Phase-by-phase implementation plans
│
└── README.md
```

### Module Dependency Graph

```
go.work
  ├── pkg/                          (shared types, interfaces, LLM router, A2A protocol)
  ├── services/api/                 (depends on pkg via replace directive)
  ├── services/orchestrator/        (depends on pkg via replace directive)
  └── test/integration/             (depends on pkg + services via replace directives)
```

Each service module uses `replace github.com/f1xgun/onevoice/pkg => ../../pkg` for local development.

---

## 5. Architecture Overview

### High-Level Flow

```
User (Web UI / future: Telegram Bot)
    │
    ▼ HTTPS
┌──────────────────────────────────────────────────────────────────────┐
│  API Service (services/api, :8080)                                    │
│  Auth (JWT), Business CRUD, Integration CRUD, Conversation CRUD       │
│  chi router + middleware (CORS, rate limiting, request logging)        │
└──────────┬───────────────────────────────────────────────────────────┘
           │
           ▼ HTTPS
┌──────────────────────────────────────────────────────────────────────┐
│  Orchestrator Service (services/orchestrator, :8090)                   │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │  LLM Router (pkg/llm/router.go)                                 │ │
│  │  Strategy selection: StrategyCost / StrategySpeed                │ │
│  │  Rate limiting (Redis) → Provider selection → Billing tracking   │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │ │
│  │  │ OpenRouter    │  │ OpenAI       │  │ Anthropic            │  │ │
│  │  │ (primary)     │  │ (direct)     │  │ (direct)             │  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│  Agent Loop: system prompt → LLM → tool calls → execute → loop       │
│  SSE streaming: POST /chat/{conversationID} → Server-Sent Events     │
│  Tool Registry: filters tools by active integrations                  │
└──────────┬───────────────────────────────────────────────────────────┘
           │ A2A Protocol (ToolRequest/ToolResponse)
           ▼
    NATS (Message Bus)
    │   Subjects:
    │   • tasks.telegram          — NATS request-reply to Telegram agent
    │   • tasks.vk                — NATS request-reply to VK agent
    │   • tasks.yandex_business   — NATS request-reply to Yandex.Business agent
    │
    ├── VK Agent (A2A, API) ──────────────► VK API
    ├── Telegram Agent (A2A, API) ────────► Telegram Bot API
    └── Yandex.Business Agent (A2A, RPA) ─► Playwright ──► business.yandex.ru
```

### A2A Communication Flow

```
Orchestrator                    NATS                    Platform Agent
     │                           │                           │
     │  ToolRequest (JSON)       │                           │
     │──────────────────────────►│──────────────────────────►│
     │  {task_id, tool, args,    │  subject: tasks.{agent}   │
     │   business_id}            │                           │
     │                           │                           │──► Execute tool
     │                           │                           │    (API call or
     │                           │                           │     RPA action)
     │  ToolResponse (JSON)      │                           │
     │◄──────────────────────────│◄──────────────────────────│
     │  {task_id, success,       │  NATS reply               │
     │   result/error}           │                           │
```

### Key Architectural Principles

1. **Platform-agnostic core**: The orchestrator, message bus, data layer, and API are completely independent of specific platforms. Agents are pluggable.
2. **Uniform agent interface (A2A)**: All agents (API-based and RPA-based) implement the `a2a.Handler` interface and communicate via `ToolRequest`/`ToolResponse` over NATS. The orchestrator dispatches standardized requests regardless of whether the agent uses REST API or browser automation.
3. **Hybrid integration encapsulated in agents**: The method of integration (API/RPA) is an internal implementation detail of each agent.
4. **Dynamic tool registration**: The LLM only sees tools for currently active/connected integrations. Tools named `{platform}__{action}` are filtered by active integrations; internal tools (no `__` prefix) are always available.
5. **Partial failure tolerance**: If one platform fails, others continue. User gets a per-platform status report.
6. **Multi-provider LLM routing**: The Router selects the optimal LLM provider (OpenRouter/OpenAI/Anthropic) based on cost or speed strategy, with health monitoring, rate limiting, and billing.

---

## 6. Agent Specifications

### 6.1 Unified Agent Interface (A2A Protocol)

Every agent is built on the `pkg/a2a.Agent` base, which provides:

- **Identity**: `AgentID` constant (e.g., `a2a.AgentTelegram = "telegram"`)
- **Transport**: Subscribes to NATS subject `tasks.{agentID}` via the `Transport` interface
- **Handler**: Implements `a2a.Handler` interface: `Handle(ctx, ToolRequest) → (*ToolResponse, error)`
- **Protocol types**:
  - `ToolRequest`: `{task_id, tool, args, business_id, request_id}`
  - `ToolResponse`: `{task_id, success, result, error}`
- **Tool naming convention**: `{platform}__{action}` (e.g., `telegram__send_post`, `vk__publish_post`)
- **Execution flow**: NATS message → JSON decode → Handler.Handle() → JSON encode → NATS reply
- **Error handling**: Handler errors automatically wrapped into `ToolResponse{Success: false, Error: ...}`

### 6.2 Yandex.Business Agent (RPA) — MVP

- **No public API exists** (as of Feb 2026)
- **Web interface**: `https://business.yandex.ru`
- **Auth**: Cookie-based session (Yandex ID login)
- **Integration method**: Playwright (headless Chromium via `playwright-go`)
- **Target operations**:
  - Update business hours (including holidays)
  - Update contact info (phone, website, description)
  - Upload photos
  - Read reviews (page scraping)
  - Reply to reviews (form automation)
- **Technical requirements**:
  - Resilient CSS/XPath selectors with fallback strategy
  - Human-like delays (random 1–5s between actions)
  - Screenshot-on-error for diagnostics
  - Retry with exponential backoff
  - Canary check before operations (verify expected DOM elements exist)
  - Stealth mode for anti-bot protection
- **Constraints**:
  - Changes go through Yandex moderation (up to 3 days)
  - RPA is fragile — DOM changes can break automation
  - Yandex anti-bot protection may interfere
  - ToS may restrict automated access
- **Tool naming convention**: `yandex_business__update_hours`, `yandex_business__reply_review`, etc.

### 6.3 VK Agent (API) — MVP

- **Official API**: `https://dev.vk.com/`
- **Auth**: OAuth 2.0
  - auth_url: `https://oauth.vk.com/authorize`
  - token_url: `https://oauth.vk.com/access_token`
  - Typical scopes: `wall`, `photos`, `groups`, `offline`
- **Target operations**:
  - Publish posts to community wall (text + media), pin if needed
  - Update community info (description, links, contacts)
  - Read comments and reply
  - Basic statistics (if available for community type/permissions)
- **Constraints**:
  - User must be admin/editor of the community
  - Rate limits depend on token/method — implement queue + backoff
  - Many actions depend on community settings
- **Tool naming convention**: `vk__publish_post`, `vk__update_group_info`, etc.

### 6.4 Telegram Agent (API) — MVP

- **Official API**: Telegram Bot API (`https://core.telegram.org/bots/api`)
- **Auth**: Bot Token (not OAuth)
- **Setup flow**:
  - User creates bot via @BotFather
  - Adds bot to channel as admin
  - Privacy mode considerations for group message reading
- **Target operations (MVP)**:
  - Publish messages to channel (text + media)
  - Edit and pin channel messages
  - Send private notifications to business owner
- **Constraints**:
  - Bot can only post where it has admin rights
  - 429 errors with `retry_after` — need send queue with backoff
- **Tool naming convention**: `telegram__send_channel_post`, `telegram__send_notification`, etc.

### 6.5 Future Agents (NOT in MVP)

| Agent | Phase | Type | Notes |
|-------|-------|------|-------|
| **2GIS** | Phase 2 | RPA (Playwright) | `account.2gis.com`, similar architecture to Yandex.Business |
| **Avito** | Phase 2 | API (limited) | Messenger API only initially |
| **Google Business Profile** | Phase 3 | API | `https://developers.google.com/my-business`, for international markets |

---

## 7. LLM Orchestrator

The orchestrator consists of two main parts: the **LLM Router** (`pkg/llm/`) which handles provider selection, rate limiting, and billing; and the **Orchestrator Service** (`services/orchestrator/`) which runs the agent loop and exposes SSE endpoints.

### 7.1 LLM Router (`pkg/llm/router.go`)

The Router dispatches LLM requests to the best available provider based on strategy, health status, and availability.

**Provider Interface** (`pkg/llm/provider.go`):
```go
type Provider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
    ListModels(ctx context.Context) ([]ModelInfo, error)
    HealthCheck(ctx context.Context) error
    Name() string
}
```

**Provider Adapters** (`pkg/llm/providers/`):

| Provider | Package | Base URL | Notes |
|----------|---------|----------|-------|
| **OpenRouter** (primary) | `providers.NewOpenRouter(apiKey)` | `https://openrouter.ai/api/v1` | Proxies OpenAI + Anthropic models; uses `go-openai` SDK |
| **OpenAI** (direct) | `providers.NewOpenAI(apiKey)` | Default OpenAI | Full tool/function calling support |
| **Anthropic** (direct) | `providers.NewAnthropic(apiKey)` | Default Anthropic | System message separation, SSE streaming |

**Routing Strategies**:
- `StrategyCost` (default): Selects provider with lowest `(InputCostPer1MTok + OutputCostPer1MTok) / 2`
- `StrategySpeed`: Selects provider with lowest `AvgLatencyMs` (entries with `AvgLatencyMs == 0` ranked last)
- Entries where `HealthStatus != "healthy"` or `Enabled == false` are always skipped

**Model Registry** (`pkg/llm/registry.go`):
- Thread-safe mapping: `model → []ModelProviderEntry`
- Each entry tracks: provider name, input/output cost per 1M tokens, average latency, health status, priority
- `RecordSuccess(provider, model, latency)`: Updates rolling latency average (100-sample window), restores health after 3 consecutive successes
- `RecordFailure(provider, model)`: Updates failure rate; >50% → "down", >20% → "degraded"

**Rate Limiting** (`pkg/llm/ratelimit.go`):
- Redis-based per-user limits using counters with TTL
- Checks: requests/minute, tokens/minute, tokens/month
- Tier-based limits:

| Tier | Requests/min | Tokens/min | Tokens/month | Daily spend |
|------|-------------|------------|--------------|-------------|
| free | 10 | 5,000 | 100,000 | $1.00 |
| basic | 60 | 50,000 | 1,000,000 | $10.00 |
| pro | 120 | 100,000 | unlimited | $50.00 |
| enterprise | unlimited | unlimited | unlimited | unlimited |

**Billing** (`pkg/llm/billing.go`):
- `UsageLog` records: user, model, provider, tokens, costs (provider + commission + user total)
- Commission modes: `percentage` (20% default), `flat` ($0.001/req), `tiered` (free=30%, basic=20%, pro=10%, enterprise=5%)
- `BillingRepository` interface: `LogUsage`, `GetUserBalance`, `GetDailySpend`, `GetMonthlyUsage`

**Router Configuration** (functional options):
```go
router := llm.NewRouter(registry,
    llm.WithProvider(openRouterProvider),
    llm.WithProvider(openAIProvider),
    llm.WithRateLimiter(rateLimiter),
    llm.WithBilling(billingRepo),
    llm.WithCommission(commissionConfig),
)
```

### 7.2 System Prompt Builder (`services/orchestrator/internal/prompt/`)

The `prompt.Build(ctx BusinessContext, history []llm.Message)` function constructs a `[]llm.Message` slice with:
1. **System message** containing:
   - Business profile (name, category, address, phone, description)
   - Current date/time
   - Tone of voice (default: "профессиональный")
   - Active integrations list
   - Behavioral rules (plan first, partial failure tolerance, Russian language)
2. **Conversation history** appended after the system message

### 7.3 Tool Registry (`services/orchestrator/internal/tools/`)

- Tools named `{platform}__{action}` are visible only if `platform` is in the active integrations list
- Tools without `__` (internal tools) are always available
- Each tool has an `Executor` interface that dispatches via NATS (`NATSExecutor`) or runs locally
- `ExecutorFunc` adapter for inline tool implementations

### 7.4 Agent Loop (`services/orchestrator/internal/orchestrator/`)

```
Input: RunRequest {UserID, Model, BusinessContext, Messages, ActiveIntegrations, Tier}
Output: <-chan Event

Loop (max 10 iterations, configurable):
  1. Filter available tools by active integrations
  2. Build system prompt from BusinessContext + conversation history
  3. Call LLM Router with messages + tools
  4. If LLM returns tool_calls:
     a. Emit EventToolCall for each tool call
     b. Parse JSON arguments → Execute via tool registry
     c. Append assistant message (with tool calls) + tool result messages
     d. Continue loop
  5. If LLM returns text (finish_reason="stop"):
     a. Emit EventText with content
     b. Emit EventDone
     c. Exit loop
  6. If max iterations reached → Emit EventError

Event types: EventText, EventToolCall, EventError, EventDone
```

### 7.5 SSE Chat Handler (`services/orchestrator/internal/handler/`)

- `POST /chat/{conversationID}` — accepts `{"model": "...", "message": "..."}`
- Streams Server-Sent Events back to the client:

```
data: {"type":"text","content":"Привет! Чем могу помочь?"}

data: {"type":"tool_call","tool_name":"telegram__send_post","tool_args":{"text":"..."}}

data: {"type":"done"}
```

- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`

### 7.6 NATS Executor (`services/orchestrator/internal/natsexec/`)

- Implements `tools.Executor` interface
- Wraps tool call arguments into `a2a.ToolRequest` with generated UUID `task_id`
- Sends via NATS request-reply to subject `tasks.{agentID}`
- Decodes `a2a.ToolResponse` reply
- Returns `resp.Result` on success, error on failure
- `Requester` interface abstracts NATS connection for testability
- `NATSConn` wraps `*nats.Conn` with context-aware request timeout

---

## 8. Platform Integrations

### Integration Classification

| Platform | Method | Priority | Write Capabilities | Read Capabilities | Complexity |
|----------|--------|----------|-------------------|-------------------|-----------|
| **VK** | ✅ API (Official) | MVP | Posts, community info | Comments, stats | Medium |
| **Telegram** | ✅ API (Bot API) | MVP | Channel messages | — | Low |
| **Yandex.Business** | 🤖 RPA (Playwright) | MVP | Hours, info, photos, review replies | Reviews, profile data | High |
| **2GIS** | 🤖 RPA (Playwright) | Phase 2 | Hours, info, contacts | Profile data | High |
| **Avito** | ⚠️ API (limited) | Phase 2 | Depends on access | Messenger API | Medium-High |
| **Google Business** | ✅ API (Official) | Phase 3 | Hours, info, posts, review replies | Reviews, metrics | High |

### Integration Type Legend

- **✅ API agents**: Use official HTTP APIs. Reliable, stable, predictable.
- **🤖 RPA agents**: Use browser automation (Playwright). Less stable (DOM-dependent), require monitoring and selector updates.
- **⚠️ Limited API**: API exists but access restricted (requires approval/contract).

> **Instagram (Meta)**: EXCLUDED entirely. Meta is banned in Russia as extremist organization.

---

## 9. Database Schema

The system uses a **dual database** architecture:
- **PostgreSQL** for relational/ACID data (users, businesses, integrations, subscriptions)
- **MongoDB** for flexible-schema/high-write data (conversations, messages, tasks, reviews, posts)

### PostgreSQL Tables (8 tables + triggers)

Migration file: `migrations/postgres/000001_init.up.sql`

| # | Table | Purpose | Key Columns |
|---|-------|---------|-------------|
| 1 | `users` | Authentication & roles | `id` UUID PK, `email` UNIQUE, `password_hash`, `role` (owner/admin/member), timestamps |
| 2 | `businesses` | Business profiles | `id` UUID PK, `user_id` FK→users, `name`, `category`, `address`, `phone`, `description`, `logo_url`, `settings` JSONB, timestamps |
| 3 | `business_schedules` | Operating hours | `id` UUID PK, `business_id` FK, `day_of_week` (0-6), `open_time`, `close_time`, `is_closed`, `special_date` (nullable), UNIQUE(business_id, day_of_week, special_date) |
| 4 | `integrations` | Platform connections | `id` UUID PK, `business_id` FK, `platform`, `status` (pending/active/error), `encrypted_access_token` BYTEA, `encrypted_refresh_token` BYTEA, `external_id`, `metadata` JSONB, `token_expires_at`, UNIQUE(business_id, platform) |
| 5 | `subscriptions` | Billing plans | `id` UUID PK, `user_id` FK, `plan` (free/pro/enterprise), `status` (active/expired), `expires_at`, timestamps |
| 6 | `audit_logs` | Security audit trail | `id` UUID PK, `user_id` FK, `action`, `resource`, `details` JSONB, `created_at`. Append-only. |
| 7 | `refresh_tokens` | JWT refresh tokens (Redis backup) | `id` UUID PK, `user_id` FK, `token_hash` UNIQUE, `expires_at`, `created_at` |
| 8 | `platform_sync_status` | Sync conflict detection | `id` UUID PK, `business_id`, `platform`, `field`, `local_value`, `remote_value`, `status` (synced/conflict), `checked_at` |

**Automatic triggers**: `update_updated_at()` on `users`, `businesses`, `integrations`, `subscriptions`.

**Indexes**: `users(email)`, `businesses(user_id)`, `business_schedules(business_id)`, `integrations(business_id)`, `integrations(platform)`, `subscriptions(user_id)`, `audit_logs(user_id)`, `audit_logs(created_at)`, `refresh_tokens(user_id)`, `refresh_tokens(token_hash)`.

### MongoDB Collections (5 collections)

Index file: `migrations/mongo/init.js`

| # | Collection | Purpose | Key Fields | Indexes |
|---|-----------|---------|------------|---------|
| 1 | `conversations` | Chat sessions | `_id`, `user_id`, `title`, `created_at`, `updated_at` | `{user_id: 1, updated_at: -1}` |
| 2 | `messages` | Chat messages with LLM interactions | `_id`, `conversation_id`, `role`, `content`, `tool_calls[]`, `tool_results[]`, `attachments[]`, `metadata`, `created_at` | `{conversation_id: 1, created_at: 1}` |
| 3 | `tasks` | Agent execution logs | `_id`, `business_id`, `type`, `status`, `platform`, `input`, `output`, `error`, `started_at`, `completed_at` | `{business_id: 1, created_at: -1}`, `{status: 1}` |
| 4 | `reviews` | Platform reviews (varied schemas per platform) | `_id`, `business_id`, `platform`, `external_id`, `author_name`, `rating`, `text`, `reply_text`, `reply_status`, `platform_meta` | `{business_id: 1, platform: 1, created_at: -1}`, `{external_id: 1, platform: 1}` UNIQUE |
| 5 | `posts` | Published content across platforms | `_id`, `business_id`, `content`, `media_urls[]`, `platform_results{}`, `status`, `scheduled_at`, `published_at` | `{business_id: 1, created_at: -1}`, `{status: 1, scheduled_at: 1}` |

**Why MongoDB for these entities**: Varied schemas per platform (review/post structures differ), nested documents (messages with tool calls, attachments), high write volume, no complex joins needed.

### Domain Models (`pkg/domain/`)

**PostgreSQL models** (`models.go`): `User`, `Business`, `BusinessSchedule`, `Integration`, `Subscription`, `AuditLog`, `RefreshToken`

**MongoDB models** (`mongo_models.go`): `Conversation`, `Message`, `Attachment`, `ToolCall`, `ToolResult`, `AgentTask`, `Review`, `Post`, `PlatformResult`

**Repository interfaces** (`repository.go`): `UserRepository`, `BusinessRepository`, `BusinessScheduleRepository`, `IntegrationRepository`, `ConversationRepository`, `MessageRepository`

### Total: 8 PostgreSQL tables + 5 MongoDB collections = 13 entities (exceeds thesis minimum of 10)

### Minimum Requirements (Thesis)

- **≥ 10 database tables/collections** — met with 13
- **≥ 500 test records** (seeded data)
- See §17 for full thesis deliverable minimums

---

## 10. REST API Specification

### API Service (`services/api`, port 8080)

**Base URL**: `/api/v1`

**Authentication**: Bearer JWT (access token, HMAC-SHA256, 15 min expiry) + refresh token (Redis-backed, 7 day expiry, rotation on use)

**Response format**: Direct JSON (no envelope). Errors mapped via HTTP status codes.

**Middleware stack** (chi): RequestID → Logger (slog) → Recoverer → CORS → Rate Limiting (Redis) → JWT Auth (protected routes)

#### Phase 1 Endpoints (15 total, implemented)

| Group | Method | Endpoint | Status | Response | Notes |
|-------|--------|----------|--------|----------|-------|
| **Auth** | POST | `/auth/register` | 201 | `{user, accessToken, refreshToken}` | Creates user + business |
| **Auth** | POST | `/auth/login` | 200 | `{user, accessToken, refreshToken}` | Verifies bcrypt hash |
| **Auth** | POST | `/auth/refresh` | 200 | `{accessToken, refreshToken}` | Rotates refresh token |
| **Auth** | POST | `/auth/logout` | 204 | — | Deletes refresh from Redis |
| **Auth** | GET | `/auth/me` | 200 | `{user}` | 🔒 JWT required |
| **Business** | GET | `/business` | 200 | `{business}` | 🔒 Returns user's business |
| **Business** | PUT | `/business` | 200 | `{business}` | 🔒 Update profile |
| **Business** | GET | `/business/schedule` | 200 | `{schedules[]}` | 🔒 Operating hours |
| **Business** | PUT | `/business/schedule` | 200 | `{schedules[]}` | 🔒 Update hours |
| **Integrations** | GET | `/integrations` | 200 | `{integrations[]}` | 🔒 List by business |
| **Integrations** | POST | `/integrations/{platform}/connect` | 302 | Redirect to OAuth | 🔒 OAuth flow initiation |
| **Integrations** | DELETE | `/integrations/{platform}` | 204 | — | 🔒 Disconnect |
| **Conversations** | GET | `/conversations` | 200 | `{conversations[]}` | 🔒 List user's conversations |
| **Conversations** | POST | `/conversations` | 201 | `{conversation}` | 🔒 Create new conversation |
| **Conversations** | GET | `/conversations/{id}` | 200 | `{conversation, messages[]}` | 🔒 With message history |

**Error mapping**:

| Domain Error | HTTP Status |
|-------------|-------------|
| `ErrUserNotFound` / `ErrBusinessNotFound` | 404 |
| `ErrUserExists` | 409 |
| `ErrInvalidCredentials` | 401 |
| `ErrUnauthorized` / `ErrInvalidToken` | 401 |
| `ErrForbidden` | 403 |
| Validation errors | 400 |
| Unknown errors | 500 (logged, details not exposed) |

### Orchestrator Service (`services/orchestrator`, port 8090)

| Method | Endpoint | Purpose | Response |
|--------|----------|---------|----------|
| POST | `/chat/{conversationID}` | Send message, receive SSE stream | `text/event-stream` |
| GET | `/health` | Health check | `{"status":"ok"}` |

#### Future API Endpoints (not yet implemented)

| Group | Method | Endpoint | Purpose |
|-------|--------|----------|---------|
| **Business** | POST | `/business/sync` | Trigger sync to all platforms |
| **Reviews** | GET | `/reviews` | Collect reviews from connected platforms |
| **Reviews** | POST | `/reviews/{id}/reply` | Publish review reply |
| **Reviews** | POST | `/reviews/{id}/generate-reply` | Generate AI draft reply |
| **Tasks** | GET | `/tasks` | List/filter tasks |
| **Tasks** | GET | `/tasks/{id}` | Get specific task status |

### Async Operations

Sync/bulk operations return a `task_id` and execute in background. UI polls for progress per platform.

---

## 11. Frontend Requirements

### Page Structure

```
/                           → Landing page (public)
/login                      → Login
/register                   → Registration

/app                        → Redirect to /app/chat
/app/chat                   → Main chat with assistant
/app/chat/:conversationId   → Specific conversation

/app/business               → Business profile
/app/integrations           → Integration management
/app/reviews                → Review feed
/app/posts                  → Post history
/app/tasks                  → Task history
/app/settings               → Account settings
```

### Chat UI Requirements

- Message history (user/assistant) with timestamps
- Support text + image attachments
- "Assistant thinking/executing" indicator
- Per-platform action results displayed as compact cards (success/error)
- Auto-scroll to latest message
- Zero-state: quick command hints (reviews, hours, post)
- Sidebar: platform connection statuses (🟢/🟡/🔴)

### Integration Management Page

- List of available platforms with connect/disconnect buttons
- For API platforms: OAuth flow
- For RPA platforms: Session/cookie setup instructions
- Status indicator per platform

### Minimum Requirements (Thesis)

- **≥ 20 UI forms/screens**
- See §17 for full thesis deliverable minimums

---

## 12. Infrastructure & Deployment

### Local Development (Docker Compose)

Services:
1. `api` — API Gateway + Core API (Go, :8080)
2. `orchestrator` — LLM Orchestrator (Go, :8090)
3. `agent-vk` — VK Agent (Go)
4. `agent-telegram` — Telegram Agent (Go)
5. `agent-yandex-business` — Yandex.Business RPA Agent (Go + Playwright)
6. `postgres` — PostgreSQL 16
7. `mongodb` — MongoDB 7
8. `redis` — Redis 7
9. `nats` — NATS (request-reply for A2A)
10. `minio` — MinIO (S3)
11. `web` — Next.js frontend (dev server)

### Environment Variables (per service)

Each service uses environment variables for configuration. Sensitive values (API keys, tokens, encryption keys) must never be committed.

**API Service** (`services/api`):
- `DATABASE_URL` — PostgreSQL connection string
- `MONGODB_URL` — MongoDB connection string (default: `mongodb://onevoice:onevoice_dev@localhost:27017/onevoice?authSource=admin`)
- `REDIS_URL` — Redis connection string
- `JWT_SECRET` — HMAC-SHA256 signing key for JWT access tokens
- `ENCRYPTION_KEY` — AES-256-GCM key (32 bytes) for token encryption at rest
- `VK_APP_ID`, `VK_APP_SECRET` — VK OAuth credentials
- `TELEGRAM_BOT_TOKEN` — Telegram bot token
- `MINIO_*` — MinIO credentials and endpoint

**Orchestrator Service** (`services/orchestrator`):
- `PORT` — HTTP port (default: `8090`)
- `LLM_MODEL` — Model identifier (required, e.g., `gpt-4o-mini`, `claude-3-5-sonnet`)
- `LLM_TIER` — User subscription tier for rate limiting (default: `free`)
- `NATS_URL` — NATS connection string (default: `nats://localhost:4222`)
- `OPENROUTER_API_KEY` — OpenRouter API key (primary provider)
- `OPENAI_API_KEY` — Direct OpenAI API key (optional)
- `ANTHROPIC_API_KEY` — Direct Anthropic API key (optional)
- `BUSINESS_NAME` — Business name for system prompt (static for now)

### Production (Kubernetes)

- All services as stateless deployments (except DB/Redis/MinIO)
- PostgreSQL: managed service or StatefulSet with replication
- Horizontal pod autoscaling for API, Orchestrator, Agents
- Ingress with TLS
- Secrets management via Kubernetes Secrets or external vault

---

## 13. Security Requirements

### Authentication & Authorization

- **JWT access token**: HMAC-SHA256, 15-minute expiry, stateless validation
  - Claims: `UserID` (UUID), `Email`, `Role`, `ExpiresAt`, `IssuedAt`
  - Sent via `Authorization: Bearer <token>` header
  - No database lookup needed to validate
- **Refresh token**: Random UUID, SHA-256 hashed, stored in Redis with 7-day TTL
  - On use: rotate to new token (old one invalidated)
  - PostgreSQL backup table for recovery (`refresh_tokens`)
  - Allows logout by deleting from Redis
- **RBAC roles** (`pkg/domain/roles.go`): `owner`, `admin`, `member`
- Logout must invalidate refresh token
- Platform-modifying operations restricted to `owner`/`admin`

### Token Storage

- Platform tokens/secrets stored **encrypted at rest** (AES-256-GCM)
- Encryption keys NOT stored in DB
- Key rotation must be supported
- Least privilege: tokens accessible only to services that need them

### Rate Limiting

- **LLM Router level** (Redis, `pkg/llm/ratelimit.go`): Per-user limits by subscription tier (requests/min, tokens/min, tokens/month, daily spend cap) — see §7.1 for tier details
- **API level** (Redis, `services/api/internal/middleware/`): Per-endpoint rate limiting (token bucket)
- **Platform level**: Per-agent outbound limits to external platform APIs (VK rate limits, Telegram 429 handling)
- Returns `ErrRateLimitExceeded` or HTTP 429 with retry-after on limit hit

### Other

- CORS configuration
- Input validation on all endpoints
- Compliance with 152-ФЗ (Russian personal data law)
- Structured logs with correlation IDs (no sensitive data in logs)

---

## 14. Non-Functional Requirements

| Requirement | Target |
|-------------|--------|
| API latency (p95) | < 500ms (excluding external platforms) |
| LLM response time (p95) | < 10s |
| Sync time per platform | < 30s (target, excluding external delays/moderation) |
| Uptime | > 99.5% |
| Planned maintenance | < 4 hours/month |
| RTO | < 1 hour |
| Horizontal scaling | Stateless services |
| Observability | Structured logs + correlation IDs, Prometheus metrics, OpenTelemetry traces |

---

## 15. Implementation Phases

### Phase 1: Backend Core ✅ COMPLETED

**Plan**: `docs/plans/2026-02-09-phase1-backend-design.md`, `docs/plans/2026-02-09-phase1-backend-implementation.md`

- [x] Go multi-module workspace (`go.work` with `pkg/`, `services/api/`, `test/integration/`)
- [x] Domain models (`pkg/domain/`): PostgreSQL entities, MongoDB entities, roles, sentinel errors, repository interfaces
- [x] Shared packages: `pkg/logger/` (slog + JSON), `pkg/crypto/` (AES-256-GCM with tests)
- [x] Database migrations: PostgreSQL 8 tables + MongoDB 5 collection indexes
- [x] PostgreSQL repositories: `UserRepository`, `BusinessRepository`, `IntegrationRepository` (squirrel + pgx)
- [x] MongoDB repositories: `ConversationRepository`, `MessageRepository`
- [x] Service layer: `UserService` (register, login, JWT, refresh), `BusinessService`, `IntegrationService`, `ConversationService`
- [x] Middleware: JWT auth, CORS, rate limiting (Redis), request logging
- [x] Handler layer: 15 API endpoints (auth 5, business 4, integrations 3, conversations 3)
- [x] Main application wiring: DB connections → repos → services → handlers → chi router → graceful shutdown
- [x] Docker Compose: PostgreSQL 16, MongoDB 7, Redis 7
- [x] Unit tests (70%+ coverage target), integration tests

**Deliverable**: Fully functional REST API with auth, business CRUD, integration management, conversation storage

### Phase 2.1–2.3: LLM Router Foundation ✅ COMPLETED

**Plans**: `docs/plans/2026-02-12-phase2-llm-router-implementation.md`, `docs/plans/2026-02-12-phase2.2-ratelimit-billing-implementation.md`, `docs/plans/2026-02-12-phase2.3-provider-adapters-implementation.md`

- [x] Provider interface (`pkg/llm/provider.go`): `Chat`, `ChatStream`, `ListModels`, `HealthCheck`
- [x] Normalized types (`pkg/llm/types.go`): `ChatRequest`, `ChatResponse`, `Message`, `ToolCall`, `StreamChunk`, `ModelInfo`
- [x] Configuration types (`pkg/llm/config.go`): YAML-based config for providers, commission, model filter, pricing
- [x] Model registry (`pkg/llm/registry.go`): Thread-safe model→provider mapping with metrics recording
- [x] Rate limiter (`pkg/llm/ratelimit.go`): Redis-based per-user limits (requests/min, tokens/min, tokens/month)
- [x] Billing types (`pkg/llm/billing.go`): `UsageLog`, `BillingRepository` interface, `CalculateCommission`
- [x] Provider adapters (`pkg/llm/providers/`):
  - OpenRouter adapter (OpenAI-compatible API, primary provider)
  - OpenAI adapter (direct, with tool/function calling support)
  - Anthropic adapter (native SDK, system message separation, SSE streaming)
- [x] Compile-time interface compliance tests

### Phase 2.4: LLM Router ✅ COMPLETED

**Plan**: `docs/plans/2026-02-13-phase2.4-llm-router-implementation.md`

- [x] Router skeleton with functional options (`WithProvider`, `WithBilling`, `WithRateLimiter`, `WithCommission`)
- [x] Strategy-based provider selection (cost vs speed) with health/enabled filtering
- [x] `RateLimitChecker` interface for testable rate limiting
- [x] Chat dispatch: rate limit check → pick provider → call → record success/failure → billing
- [x] ChatStream dispatch: same selection, no billing (token counts unavailable at stream open)
- [x] Sentinel errors: `ErrNoProvider`, `ErrRateLimitExceeded`
- [x] Comprehensive test suite: strategy selection, rate limiting, billing integration, failure recording

### Phase 2.5: Orchestrator Service ✅ COMPLETED

**Plan**: `docs/plans/2026-02-13-phase2.5-orchestrator-service.md`

- [x] Module scaffold: `services/orchestrator/go.mod`, env-based config, `go.work` integration
- [x] Prompt builder (`internal/prompt/`): `BusinessContext` → system message + history
- [x] Tool registry (`internal/tools/`): Integration-filtered tool definitions + `Executor` interface
- [x] Agent loop (`internal/orchestrator/`): LLM call → tool execution → loop → max iteration guard
- [x] SSE handler (`internal/handler/`): `POST /chat/{conversationID}` → `text/event-stream`
- [x] Main wiring (`cmd/main.go`): config → LLM Router → tools → orchestrator → chi router → serve
- [x] Tests: config, prompt builder, tool registry, agent loop (text/tool/max-iter), SSE handler

### Phase 3: A2A Agent Framework ✅ COMPLETED

**Plan**: `docs/plans/2026-02-14-phase3-a2a-framework.md`

- [x] Protocol types (`pkg/a2a/protocol.go`): `ToolRequest`, `ToolResponse`, `AgentID` constants, `Subject()` helper
- [x] Agent base (`pkg/a2a/agent.go`): `Transport` interface, `Handler`/`HandlerFunc`, `Agent.Start()` with NATS subscribe + dispatch
- [x] NATS executor (`services/orchestrator/internal/natsexec/`): `NATSExecutor` implements `tools.Executor`, `Requester` interface, `NATSConn` adapter
- [x] Wired into orchestrator: NATS connection + `registerPlatformTools` (telegram, vk, google stubs)
- [x] Config updated: `NATS_URL` env var with graceful degradation if NATS unavailable
- [x] Tests with fake transports/requesters (no real NATS required)

### Phase 4: Platform Agents — MVP (NOT YET STARTED)

**Telegram Agent**
- [ ] Standalone service using `pkg/a2a.Agent` base
- [ ] Bot setup flow (user provides token)
- [ ] `telegram__send_channel_post` tool implementation
- [ ] `telegram__send_notification` tool implementation
- [ ] NATS subscription to `tasks.telegram`

**VK Agent**
- [ ] Standalone service using `pkg/a2a.Agent` base
- [ ] OAuth flow (connect/callback/disconnect)
- [ ] `vk__publish_post` tool implementation
- [ ] `vk__update_group_info` tool implementation
- [ ] `vk__get_comments` tool implementation
- [ ] NATS subscription to `tasks.vk`

**Yandex.Business Agent (RPA)**
- [ ] Standalone service using `pkg/a2a.Agent` base
- [ ] Playwright browser automation (`playwright-go`)
- [ ] Session/cookie management (Yandex ID login)
- [ ] `yandex_business__update_hours` tool implementation
- [ ] `yandex_business__update_info` tool implementation
- [ ] `yandex_business__get_reviews` tool implementation
- [ ] `yandex_business__reply_review` tool implementation
- [ ] Resilient selectors, human-like delays, screenshot-on-error, stealth mode
- [ ] NATS subscription to `tasks.yandex_business`

**Shared RPA Utilities**
- [ ] Browser lifecycle management
- [ ] Resilient CSS/XPath selector helpers with fallback strategy
- [ ] Human-like delay utilities (random 1–5s)
- [ ] Screenshot-on-error for diagnostics
- [ ] Retry with exponential backoff
- [ ] Canary check before operations

### Phase 5: Frontend (NOT YET STARTED)

- [ ] Next.js 14 project with Tailwind + shadcn/ui
- [ ] Auth pages (login, register)
- [ ] Layout with sidebar navigation
- [ ] Chat UI (messages, input, typing indicator, SSE integration)
- [ ] Business Profile form
- [ ] Integrations page (list, connect/disconnect)
- [ ] Reviews page
- [ ] Posts page
- [ ] Task history page
- [ ] Settings page

### Phase 6: Polish & Testing (NOT YET STARTED)

- [ ] Review monitoring system
- [ ] AI review reply generation
- [ ] Sync conflict detection and resolution UI
- [ ] Comprehensive error handling and user-facing error messages
- [ ] Comparative testing: API agents vs RPA agents (execution time, reliability, resilience)
- [ ] Seed database with ≥500 test records
- [ ] Final UI polish (≥20 screens/forms)

**Deliverable**: Complete MVP ready for thesis defense

### Future (Post-thesis)

- 2GIS RPA Agent
- Avito Agent
- Google Business Profile Agent (international)
- Telegram Bot for management (alternative to web UI)
- Multi-location support
- Advanced analytics
- Content generation

---

## 16. Risks & Constraints

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
| **Platform API changes** | High | High | Isolate logic in agents, monitor changelogs, version pinning |
| **Blocking for automation** | Medium | Critical | Rate limiting, human-like behavior, ToS compliance |
| **OAuth token expiry** | 100% | Medium | Auto-refresh, user notifications, graceful degradation |
| **High LLM costs** | Medium | High | Response caching, prompt optimization, local models for simple tasks |
| **LLM hallucinations** | Medium | Medium | Action validation, sandbox for dangerous ops, human-in-the-loop for critical |
| **RPA fragility (Yandex.Business, 2GIS)** | High | High | Resilient selectors, canary checks, screenshot-on-error, auto-alerts on breakage; fallback to manual mode |
| **Anti-bot protection (RPA)** | Medium | High | Human-like delays, Playwright stealth mode, ToS compliance, rate limiting |
| **Instagram/Meta legal risk** | — | — | Fully excluded from project |

### Hard Constraints

1. **Language**: All user-facing text in Russian
2. **No Instagram**: Legally prohibited in Russia
3. **Google Business is NOT MVP**: Phase 3 only
4. **Yandex.Business IS MVP**: Despite no API — must use RPA
5. **Test instances for evaluation**: No real business required; create test VK community, Telegram channel, Yandex.Business card
6. **Thesis deadline**: Implementation must be complete for defense

---

## 17. Thesis Deliverable Minimums

The implementation serves as a thesis project (ВКР). These are hard minimums that **must be met**:

| Deliverable | Minimum | Notes |
|-------------|---------|-------|
| Database tables | 10 | See §9 for entity list |
| Database records (seed) | 500 | Realistic test data |
| UI forms/screens | 20 | All pages in §11 count |
| User roles | 3 | `owner`, `admin`, `member` |
| Algorithms (documented) | 5 | Orchestrator loop, sync, conflict detection, review analysis, RPA canary check, etc. |
| Functions/methods | 30+ | Across all services |
| Pages (thesis document) | 80+ | Excluding appendices |
| Sources (bibliography) | 50+ | ≤50% electronic, ≥5% foreign-language |

### Scientific Novelty (must be demonstrated in code)

1. **Platform-agnostic multi-agent architecture** with hybrid integration (API + RPA)
2. **Platform classification** by integration type + corresponding agent design patterns
3. **Comparative analysis** of API vs RPA agent technical characteristics (execution time, reliability, resilience) on test instances

---

## Appendix A: Key External References

| Resource | URL |
|----------|-----|
| VK API Docs | `https://dev.vk.com/` |
| Telegram Bot API | `https://core.telegram.org/bots/api` |
| Yandex.Business (web) | `https://business.yandex.ru` |
| 2GIS Account (web) | `https://account.2gis.com` |
| Avito API | `https://developers.avito.ru/about-api` |
| Google Business Profile API | `https://developers.google.com/my-business` |
| A2A Spec (Google, inspiration) | `https://google.github.io/A2A/` |
| NATS | `https://nats.io/` |
| OpenRouter API | `https://openrouter.ai/docs` |
| OpenAI API | `https://platform.openai.com/docs/api-reference` |
| Anthropic API | `https://docs.anthropic.com/` |
| go-openai SDK | `https://github.com/sashabaranov/go-openai` |
| anthropic-sdk-go | `https://github.com/anthropics/anthropic-sdk-go` |
| Playwright for Go | `https://github.com/playwright-community/playwright-go` |

## Appendix B: Market Context (Russia)

- **Yandex Maps**: 53% usage for finding local businesses (ROMIR, May 2025)
- **2GIS**: 44% usage (ROMIR, May 2025)
- **Google Maps**: 3% usage in Russia
- **63%** of Russian residents use geo-services to find offline businesses
- **48%** use maps specifically to check business hours
- **59%** consider ratings, **51%** consider reviews when choosing in maps
- **Instagram**: Blocked since March 2022; Meta designated extremist organization; advertising prohibition since Sep 2025
