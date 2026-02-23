# OneVoice Architecture

## System Overview

OneVoice is a platform-agnostic multi-agent system for automating digital presence management.
It uses a hybrid integration model: API-based agents for platforms with public APIs, and RPA-based agents for platforms without.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Frontend (Next.js)                         │
│                           :3000                                     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────────────┐ │
│  │ Auth     │ │ Business │ │ Integr.  │ │ Chat (SSE)             │ │
│  │ Pages    │ │ Settings │ │ Mgmt     │ │ AI Assistant           │ │
│  └──────────┘ └──────────┘ └──────────┘ └────────────────────────┘ │
└──────────────────────┬──────────────────────┬──────────────────────┘
                       │ REST /api/v1/*        │ SSE /chat/*
                       ▼                       ▼
┌──────────────────────────────┐  ┌──────────────────────────────────┐
│       API Service            │  │     Orchestrator Service         │
│       :8080                  │  │     :8090                        │
│                              │  │                                  │
│  Handler → Service → Repo   │  │  Chat Handler → Orchestrator     │
│                              │  │       ↓                          │
│  ┌─────────┐ ┌───────────┐  │  │  LLM Router (multi-provider)    │
│  │PostgreSQL│ │ MongoDB   │  │  │       ↓                          │
│  │Users     │ │Convos     │  │  │  Tool Registry → NATS Executor  │
│  │Business  │ │Messages   │  │  │                                  │
│  │Integr.   │ │           │  │  └──────────┬───────────────────────┘
│  └─────────┘ └───────────┘  │              │
│  ┌─────────┐                │              │ NATS (tasks.{agentID})
│  │ Redis   │                │              │
│  │Sessions │                │              ▼
│  │RateLimit│                │  ┌───────────────────────────────────┐
│  └─────────┘                │  │         Agent Layer (NATS)        │
└──────────────────────────────┘  │                                   │
                                  │  ┌─────────┐ ┌────────┐ ┌──────┐│
                                  │  │Telegram  │ │  VK    │ │YaBiz ││
                                  │  │Agent     │ │ Agent  │ │Agent ││
                                  │  │(API)     │ │ (API)  │ │(RPA) ││
                                  │  │          │ │        │ │Play- ││
                                  │  │tasks.    │ │tasks.  │ │wright││
                                  │  │telegram  │ │vk      │ │      ││
                                  │  └─────────┘ └────────┘ └──────┘│
                                  └───────────────────────────────────┘
```

## Module Map

| Module | Path | Purpose | Port |
|--------|------|---------|------|
| Shared | `pkg/` | Domain models, auth, LLM router, A2A framework | — |
| API | `services/api/` | REST API, auth, business CRUD | 8080 |
| Orchestrator | `services/orchestrator/` | LLM agent loop, tool dispatch via NATS | 8090 |
| Frontend | `services/frontend/` | Next.js dashboard | 3000 |
| Telegram Agent | `services/agent-telegram/` | Telegram Bot API integration | — |
| VK Agent | `services/agent-vk/` | VK API integration | — |
| YaBiz Agent | `services/agent-yandex-business/` | Yandex.Business RPA via Playwright | — |
| Integration Tests | `test/integration/` | End-to-end tests with Docker | — |

## Communication Patterns

### REST (Client ↔ API)
- Standard request/response over HTTP
- JSON payloads, JWT authentication
- Rate limiting via Redis token bucket

### SSE (Client ↔ Orchestrator)
- `POST /chat/{conversationID}` returns Server-Sent Events stream
- Format: `data: {"type":"...","content":"..."}\n\n`
- Events: `text`, `tool_call`, `tool_result`, `done`, `error`

### NATS (Orchestrator ↔ Agents)
- Request/reply pattern over NATS subjects
- Subject format: `tasks.{agentID}` (e.g., `tasks.telegram`)
- Payload: A2A protocol messages (JSON)
- Timeout: configurable per-request

## Data Flow: Chat Request

```
1. User sends message via frontend
2. API proxy receives POST /api/v1/chat/{id}, forwards to orchestrator
3. Orchestrator builds prompt (system + history + user message)
4. Orchestrator sends to LLM via router (OpenRouter/OpenAI/Anthropic)
5. LLM responds with text or tool_call
6. If tool_call:
   a. Orchestrator emits SSE event: {"type":"tool_call","tool_name":"...","tool_args":{}}
   b. Resolves tool → agent mapping (platform prefix → NATS subject)
   c. Sends NATS request to tasks.{agentID}
   d. Agent fetches integration token from API internal endpoint
   e. Agent executes tool (Telegram Bot API / VK API / Playwright RPA)
   f. Orchestrator emits SSE event: {"type":"tool_result","tool_name":"...","tool_result":{}}
   g. Result fed back to LLM (loop continues, max 10 iterations)
7. Final text streamed via SSE: {"type":"text","content":"..."}
8. API proxy accumulates tool_call/tool_result events, persists to MongoDB Message
```

## Conversation History

- `GET /api/v1/conversations` — list conversations
- `GET /api/v1/conversations/:id/messages` — full message history with toolCalls + toolResults
- Frontend `useChat.ts` loads history on mount; reconstructs tool call panel state from API response

## LLM Provider Stack

```
LLM Router
├── OpenRouter (primary, supports many models)
├── OpenAI (direct, GPT-4 etc.)
├── Anthropic (direct, Claude)
└── SelfHosted (OpenAI-compatible, multiple endpoints)
```

Provider selection: model name → registry lookup → provider.
Fallback: configurable per-model.

## Storage

| Store | Technology | Data |
|-------|-----------|------|
| Users, Businesses, Integrations | PostgreSQL 16 | Relational data, encrypted tokens |
| Conversations, Messages | MongoDB | Chat history, tool call logs |
| Sessions, Rate Limits | Redis 7 | Ephemeral state |
| File Uploads | MinIO (S3) | Media assets (future) |
