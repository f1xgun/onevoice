# Phase 2: LLM Orchestrator — Design Summary Document

**Document Version:** 1.0
**Date:** 2026-02-09
**Status:** Design Complete, Ready for Implementation

---

## 1. Executive Summary

The LLM Orchestrator is the intelligence layer of OneVoice that:
- Routes user requests to optimal LLM providers (OpenRouter, OpenAI, Anthropic, local models)
- Manages conversations with context window optimization
- Executes platform actions via agent tools (Telegram, VK, Google Business)
- Enforces rate limits, billing, and subscription tiers
- Streams real-time updates to frontend via SSE

**Key Innovation:** Users select only a model name — the system automatically chooses the optimal provider based on cost/speed strategy, with automatic fallback.

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Frontend (Next.js)                       │
│                     SSE Stream ← WebSocket Alt                   │
└────────────────────────────┬────────────────────────────────────┘
                             │ HTTP/SSE
                             ↓
┌─────────────────────────────────────────────────────────────────┐
│                    Orchestrator Service (Go)                     │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  HTTP Handler → Orchestrator → LLM Router                │  │
│  │       ↓              ↓              ↓                     │  │
│  │  Conversation    Tool Registry   Provider Registry       │  │
│  │    Manager         ↓                                      │  │
│  │       ↓        Tool Executor                              │  │
│  │  Context Mgr       ↓                                      │  │
│  └────────────────────┼──────────────────────────────────────┘  │
└────────────────────────┼──────────────────────────────────────┘
                         │ NATS JetStream
                         ↓
┌─────────────────────────────────────────────────────────────────┐
│                    Agent Layer (A2A Protocol)                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │   Telegram   │  │      VK      │  │    Google    │         │
│  │    Agent     │  │    Agent     │  │   Business   │         │
│  │              │  │              │  │    Agent     │         │
│  │ • send_msg   │  │ • publish    │  │ • post       │         │
│  │ • edit_msg   │  │ • edit       │  │ • reply_rev  │         │
│  │ • pin_msg    │  │ • comments   │  │ • update_hrs │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              Internal Agent (Orchestrator)                │  │
│  │  • get_business_profile  • get_recent_reviews            │  │
│  │  • get_post_analytics    • search_conversations          │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────────┐
│                    LLM Providers (External)                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  OpenRouter  │  │    OpenAI    │  │  Anthropic   │         │
│  │  (Gateway)   │  │   (Direct)   │  │   (Direct)   │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────────┐
│                      Data Layer                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  PostgreSQL  │  │   MongoDB    │  │    Redis     │         │
│  │  (Billing,   │  │ (Convos,     │  │ (Rate Limit, │         │
│  │   Users,     │  │  Messages,   │  │  Cache)      │         │
│  │   Pricing)   │  │  Tasks)      │  │              │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Core Components

### 3.1 LLM Router (`pkg/llm/router.go`)

**Responsibilities:**
- Load user context (subscription, balance, promos, rate limits)
- Pre-flight checks (balance, rate limits, daily spend)
- Select optimal provider by strategy (cost/speed)
- Execute LLM request with automatic fallback
- Post-process (billing, usage logging, metrics)

**Key Methods:**
```go
Chat(ctx, ChatRequest) (*ChatResponse, error)
ChatStream(ctx, ChatRequest) (<-chan StreamChunk, error)
```

**Decision Flow:**
1. Get user's subscription tier and balance
2. Filter providers by: model support, health status, tier permissions
3. Score providers by strategy (cost = price per token, speed = latency)
4. Sort by score, attempt primary, fallback on failure
5. Calculate cost with commission, deduct from balance, log usage

### 3.2 Provider Registry (`pkg/llm/registry.go`)

**Responsibilities:**
- Store model-provider pairs with pricing metadata
- Track provider health and performance metrics
- Dynamic discovery from provider APIs
- Admin controls (enable/disable specific pairs)

**Data Model:**
```go
type ModelProviderEntry struct {
    Model              string    // "claude-3.5-sonnet-20241022"
    Provider           string    // "openrouter", "openai", "anthropic"
    InputCostPer1MTok  float64   // Provider cost for input
    OutputCostPer1MTok float64   // Provider cost for output
    AvgLatencyMs       int       // Rolling average latency
    HealthStatus       string    // "healthy", "degraded", "down"
    Enabled            bool      // Admin toggle
    Priority           int       // Manual override
}
```

**Configuration (`config/llm.yaml`):**
- Provider credentials via env vars
- Model whitelist/blacklist
- Pricing overrides
- Commission settings (percentage, flat, tiered)

### 3.3 Rate Limiter (`pkg/llm/ratelimit.go`)

**Redis-based counters with TTL:**
- `ratelimit:{userID}:requests:min` → expires after 1 minute
- `ratelimit:{userID}:tokens:min` → expires after 1 minute
- `ratelimit:{userID}:tokens:month` → expires at month end

**Subscription tier limits:**
```go
TierConfigs = {
    "free":       {10 req/min, 5K tok/min, 100K/month, $1/day},
    "basic":      {60 req/min, 50K tok/min, 1M/month, $10/day},
    "pro":        {120 req/min, 100K tok/min, unlimited, $50/day},
    "enterprise": {unlimited, unlimited, unlimited, unlimited},
}
```

**Promo multipliers:**
- `new_user_trial`: +$5 credits, 14 days
- `subscription_bonus`: +$10 credits on first subscription
- `black_friday`: 2x rate limits for 7 days

### 3.4 Billing System (`pkg/llm/billing.go`)

**Cost breakdown:**
```go
type CostBreakdown struct {
    ProviderCost float64  // Actual cost to LLM provider
    Commission   float64  // OneVoice markup
    UserCost     float64  // Total charged to user (providerCost + commission)
}
```

**Commission modes:**
- `percentage`: 20% markup (configurable per tier)
- `flat`: $0.001 per request
- `tiered`: free=30%, basic=20%, pro=10%, enterprise=5%

**Usage logging (PostgreSQL):**
```sql
CREATE TABLE llm_usage_logs (
    user_id UUID,
    model TEXT,
    provider TEXT,
    input_tokens INT,
    output_tokens INT,
    provider_cost_usd DECIMAL(8,4),
    commission_usd DECIMAL(8,4),
    user_cost_usd DECIMAL(8,4),
    user_tier TEXT,
    created_at TIMESTAMPTZ
);
```

### 3.5 Orchestrator (`services/orchestrator/internal/orchestrator.go`)

**Agent loop:**
1. Load conversation history + business context
2. Build dynamic system prompt (business info, integrations, current time)
3. Get available tools from registry (filtered by active integrations)
4. Fit messages to context window (summarize if needed)
5. Call LLM with tools
6. If tool calls → execute via NATS → add results → loop (max 10 iterations)
7. If text response → save message → return

**Streaming:**
- Emits SSE events: `thinking`, `tool_start`, `tool_result`, `content_delta`, `done`, `error`
- Frontend displays real-time progress

### 3.6 Tool Registry (`services/orchestrator/internal/tool_registry.go`)

**Dynamic discovery:**
- Agents announce capabilities via NATS subject `agents.register`
- Orchestrator subscribes and builds registry
- On demand: `agents.announce` → all agents re-announce

**Agent card format (A2A protocol):**
```go
type AgentCard struct {
    Name        string  // "telegram-agent"
    Platform    string  // "telegram"
    Tools       []Tool  // Array of tool definitions
}

type Tool struct {
    Name        string                 // "telegram__send_message"
    Description string                 // For LLM
    Parameters  map[string]interface{} // JSON Schema
}
```

**Tool filtering:**
- Only tools for active integrations (status="active") are included
- Internal tools (get_business_profile, etc.) always available

### 3.7 Tool Executor (`services/orchestrator/internal/tool_executor.go`)

**Generic routing:**
- Lookup agent for tool in registry
- Build NATS subject: `agent.{platform}.execute`
- Send `ToolExecutionRequest` via NATS request-reply (30s timeout)
- Parse `ToolExecutionResponse`
- Return output or error

**No hardcoded logic** — all tools handled by agents.

### 3.8 Agent Base (`pkg/a2a/agent_base.go`)

**Reusable foundation for all agents:**
```go
type AgentBase struct {
    name         string
    platform     string
    toolRegistry *LocalToolRegistry  // Tool → Handler mapping
}

// Register tool with callback
func (ab *AgentBase) RegisterTool(tool Tool, handler ToolHandler)

// Generic execution handler (routes to callbacks)
func (ab *AgentBase) handleExecution(msg *nats.Msg)
```

**Example usage (Telegram Agent):**
```go
agent := a2a.NewAgentBase("telegram-agent", "telegram", nc)

agent.RegisterTool(
    a2a.Tool{
        Name: "telegram__send_message",
        Description: "Отправляет сообщение в Telegram канал",
        Parameters: {...},
    },
    func(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
        // Implementation here
        return map[string]interface{}{"message_id": 123, "status": "sent"}, nil
    },
)

agent.Start("Управление Telegram каналом", "1.0.0")
```

**No switch statements needed** — callbacks eliminate boilerplate.

### 3.9 Context Manager (`services/orchestrator/internal/context_manager.go`)

**Context window optimization:**
- Token counting via `tiktoken-go` (cl100k_base encoding)
- Max window: 120K tokens (Claude, GPT-4)
- Reserve: 4K for output, 2K for system prompt
- Strategy: Keep system prompt + recent messages + summarize middle
- Summarization: Extract key points or call LLM for compression
- Store summaries in conversation metadata

### 3.10 Conversation Manager (`services/orchestrator/internal/conversation.go`)

**MongoDB collections:**
- `conversations`: {id, user_id, title, summary, created_at, updated_at}
- `messages`: {id, conversation_id, role, content, tool_calls, tool_results, metadata, created_at}

**Operations:**
- `Load(conversationID)` → conversation + messages (sorted by created_at)
- `AppendMessage(message)` → insert message, update conversation.updated_at
- `UpdateSummary(conversationID, summary)` → store context summary
- `Create(userID, title)` → new conversation

---

## 4. Data Flow: User Message → Response

**Step-by-step:**

1. **Frontend** → POST `/api/v1/conversations/{id}/messages` with `Accept: text/event-stream`

2. **HTTP Handler** → Save user message to MongoDB

3. **Orchestrator.ChatStream()** →
   - Emit SSE: `thinking` (phase: "planning")
   - Load conversation history from MongoDB
   - Load business + integrations from API/DB
   - Build system prompt with business context
   - Get available tools from tool registry

4. **Context Manager** →
   - Count tokens in conversation
   - If > 120K, summarize middle section
   - Return fitted messages

5. **Agent Loop** (iteration 1) →
   - Emit SSE: `thinking` (phase: "executing")
   - Call `LLMRouter.Chat()` →
     - Load user context (subscription, balance, promos)
     - Pre-flight checks (balance > 0, rate limits OK)
     - Get providers for model: [{openrouter, $3/$15}, {anthropic, $3/$15}]
     - Score by strategy (cost): openrouter=9, anthropic=9 (tie → priority)
     - Attempt openrouter → success
     - Calculate cost: provider=$0.045, commission=$0.009, user=$0.054
     - Deduct $0.054 from user balance
     - Log usage to PostgreSQL
     - Update rate limit counters in Redis
     - Update registry metrics (latency, success)
   - LLM responds with tool calls: `[telegram__send_message, vk__publish_post]`

6. **Tool Execution** (parallel) →
   - Emit SSE: `tool_start` (telegram__send_message)
   - Emit SSE: `tool_start` (vk__publish_post)
   - Lookup agents: telegram-agent, vk-agent
   - Send NATS requests:
     - `agent.telegram.execute` → {business_id, tool_name, arguments}
     - `agent.vk.execute` → {business_id, tool_name, arguments}

7. **Telegram Agent** →
   - Receive NATS message
   - Lookup handler: `toolRegistry.GetHandler("telegram__send_message")`
   - Execute callback:
     - Get integration token from API
     - Create Telegram bot client
     - Send message to channel
   - Respond: {success: true, output: {message_id: 456, status: "sent"}}
   - Emit SSE: `tool_result` (success, duration: 850ms)

8. **VK Agent** →
   - Similar flow
   - Respond: {success: true, output: {post_id: 789, status: "published"}}
   - Emit SSE: `tool_result` (success, duration: 1200ms)

9. **Agent Loop** (iteration 2) →
   - Add tool results to message history
   - Call LLM again with results
   - LLM responds: "Готово! Сообщение опубликовано в Telegram и ВКонтакте."
   - No tool calls → exit loop

10. **Finalization** →
    - Emit SSE: `content_delta` (assistant's text)
    - Save assistant message to MongoDB
    - Emit SSE: `done` (message_id, cost, tokens, iterations)

11. **Frontend** → Display final message, show cost breakdown, tool execution cards

---

## 5. API Specifications

### 5.1 Orchestrator HTTP API

**Base URL:** `https://api.onevoice.app/api/v1`

#### GET `/conversations`
List user's conversations.

**Response:**
```json
{
  "conversations": [
    {
      "id": "uuid",
      "user_id": "uuid",
      "title": "Разговор 09.02.2026 14:30",
      "summary": "Discussed posting schedule...",
      "created_at": "2026-02-09T14:30:00Z",
      "updated_at": "2026-02-09T14:45:00Z"
    }
  ]
}
```

#### POST `/conversations`
Create new conversation.

**Request:**
```json
{
  "title": "Planning Q1 content"
}
```

**Response:** `201 Created` with conversation object.

#### GET `/conversations/{id}/messages`
Get conversation messages (paginated).

**Query params:** `limit=50`, `offset=0`

**Response:**
```json
{
  "messages": [
    {
      "id": "uuid",
      "role": "user",
      "content": "Опубликуй пост о скидках",
      "created_at": "2026-02-09T14:30:00Z"
    },
    {
      "id": "uuid",
      "role": "assistant",
      "content": "Готово! Опубликовал во всех каналах.",
      "tool_executions": [
        {
          "tool_name": "telegram__send_message",
          "agent_name": "telegram-agent",
          "duration_ms": 850,
          "output": {"message_id": 456, "status": "sent"}
        }
      ],
      "created_at": "2026-02-09T14:31:15Z"
    }
  ],
  "count": 2
}
```

#### POST `/conversations/{id}/messages`
Send message to conversation (SSE streaming).

**Headers:**
- `Accept: text/event-stream` (for streaming)
- `Accept: application/json` (for sync fallback)

**Request:**
```json
{
  "message": "Опубликуй пост о новом меню",
  "model": "claude-3.5-sonnet-20241022",
  "strategy": "cost"
}
```

**SSE Response:**
```
data: {"type":"thinking","data":{"phase":"planning"}}

data: {"type":"thinking","data":{"phase":"executing"}}

data: {"type":"tool_start","data":{"tool_name":"telegram__send_message","arguments":{...}}}

data: {"type":"tool_result","data":{"tool_name":"telegram__send_message","success":true,"output":{...},"duration_ms":850}}

data: {"type":"content_delta","data":{"delta":"Готово! "}}

data: {"type":"content_delta","data":{"delta":"Пост опубликован."}}

data: {"type":"done","data":{"message_id":"uuid","cost":{"provider_cost":0.045,"commission":0.009,"user_cost":0.054},"model":"claude-3.5-sonnet-20241022","provider":"openrouter","iterations":2}}
```

#### DELETE `/conversations/{id}`
Delete conversation and all messages.

**Response:** `200 OK` with `{"status": "deleted"}`

### 5.2 Agent NATS Protocol

#### Subject: `agents.register`
Agent announces capabilities on startup.

**Payload (AgentCard):**
```json
{
  "name": "telegram-agent",
  "description": "Управление Telegram каналом",
  "version": "1.0.0",
  "platform": "telegram",
  "tools": [
    {
      "name": "telegram__send_message",
      "description": "Отправляет сообщение в канал",
      "parameters": {
        "type": "object",
        "properties": {
          "text": {"type": "string", "description": "Текст"},
          "photo_url": {"type": "string"}
        },
        "required": ["text"]
      }
    }
  ]
}
```

#### Subject: `agents.announce`
Orchestrator requests all agents to re-announce.

**Payload:** `{}`

#### Subject: `agent.{platform}.execute`
Execute tool on agent (request-reply, 30s timeout).

**Request:**
```json
{
  "business_id": "uuid",
  "tool_name": "telegram__send_message",
  "arguments": {
    "text": "Новое меню уже в наличии!",
    "photo_url": "https://..."
  },
  "request_id": "uuid"
}
```

**Response:**
```json
{
  "success": true,
  "output": {
    "message_id": 456,
    "status": "sent"
  },
  "agent_name": "telegram-agent",
  "duration": 850
}
```

**Error response:**
```json
{
  "success": false,
  "error": "token expired: refresh required",
  "agent_name": "telegram-agent",
  "duration": 120
}
```

---

## 6. Configuration Files

### 6.1 `config/llm.yaml`

```yaml
providers:
  openrouter:
    enabled: true
    api_key_env: OPENROUTER_API_KEY
    priority: 1
  openai:
    enabled: true
    api_key_env: OPENAI_API_KEY
    priority: 2
  anthropic:
    enabled: true
    api_key_env: ANTHROPIC_API_KEY
    priority: 3

commission:
  mode: tiered  # percentage | flat | tiered
  percentage: 20.0
  flat_fee_usd: 0.001
  tiered:
    free: 30.0
    basic: 20.0
    pro: 10.0
    enterprise: 5.0

model_filter:
  mode: whitelist  # whitelist | blacklist | all
  whitelist:
    - claude-3.5-sonnet-20241022
    - claude-3-opus-20240229
    - gpt-4-turbo
    - gpt-4o
    - gpt-3.5-turbo

pricing_overrides:
  claude-3.5-sonnet-20241022:
    input_per_1m: 3.00
    output_per_1m: 15.00
  gpt-4-turbo:
    input_per_1m: 10.00
    output_per_1m: 30.00

default_pricing:
  input_per_1m: 5.00
  output_per_1m: 15.00
```

### 6.2 Environment Variables

```bash
# LLM Providers
OPENROUTER_API_KEY=sk-or-...
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...

# Databases
POSTGRES_URL=postgresql://user:pass@localhost:5432/onevoice
MONGODB_URL=mongodb://localhost:27017/onevoice
REDIS_URL=redis://localhost:6379

# NATS
NATS_URL=nats://localhost:4222

# Service
ORCHESTRATOR_PORT=8080
LOG_LEVEL=info
```

---

## 7. Implementation Checklist

### Phase 2.1: LLM Router & Provider Abstraction (Week 2)
- [ ] `pkg/llm/provider.go` — Provider interface
- [ ] `pkg/llm/providers/openrouter.go` — OpenRouter adapter
- [ ] `pkg/llm/providers/openai.go` — OpenAI adapter (go-openai)
- [ ] `pkg/llm/providers/anthropic.go` — Anthropic adapter (anthropic-sdk-go)
- [ ] `pkg/llm/registry.go` — Model-provider registry with dynamic discovery
- [ ] `pkg/llm/router.go` — Smart routing with scoring & fallback
- [ ] `pkg/llm/ratelimit.go` — Redis-based rate limiter
- [ ] `pkg/llm/billing.go` — BillingRepository (balance, promos, usage)
- [ ] `pkg/llm/usage_logger.go` — UsageLogger (PostgreSQL)
- [ ] `pkg/llm/init.go` — Dynamic initialization from config
- [ ] `config/llm.yaml` — Configuration file
- [ ] Unit tests for router, registry, rate limiter
- [ ] Integration test: Chat → route → provider → cost → log

### Phase 2.2: Orchestrator Core (Week 3)
- [ ] `services/orchestrator/internal/orchestrator.go` — Agent loop
- [ ] `services/orchestrator/internal/prompt_builder.go` — Dynamic system prompt
- [ ] `services/orchestrator/internal/tool_registry.go` — Dynamic tool discovery
- [ ] `services/orchestrator/internal/tool_executor.go` — Generic NATS routing
- [ ] `services/orchestrator/internal/context_manager.go` — Context window management
- [ ] `services/orchestrator/internal/conversation.go` — MongoDB ConversationManager
- [ ] `pkg/a2a/agent_base.go` — Reusable agent base with callbacks
- [ ] `pkg/a2a/tool_handler.go` — LocalToolRegistry
- [ ] Unit tests for orchestrator components
- [ ] Integration test: Full chat loop with mock agents

### Phase 2.3: Internal Agent (Week 3)
- [ ] `services/orchestrator/internal/internal_agent.go` — Internal tools agent
- [ ] Tools: get_business_profile, get_recent_reviews, get_post_analytics, search_conversations
- [ ] Register with orchestrator tool registry
- [ ] Integration test: LLM calls internal tool

### Phase 2.4: HTTP API with SSE (Week 3)
- [ ] `services/orchestrator/internal/handler/http.go` — HTTP handlers
- [ ] `services/orchestrator/internal/stream.go` — SSE event types
- [ ] Endpoints: GET/POST conversations, GET/POST messages, DELETE conversation
- [ ] SSE streaming implementation
- [ ] Frontend hook example: `useChat.ts`
- [ ] Integration test: HTTP → SSE stream → events

### Phase 2.5: Database Migrations (Week 3)
- [ ] PostgreSQL: user_balances, subscriptions, user_promos, llm_usage_logs tables
- [ ] MongoDB: conversations, messages collections + indexes
- [ ] Seed data: subscription tiers, promo templates

### Phase 2.6: Service Deployment (Week 3)
- [ ] `services/orchestrator/cmd/main.go` — Service entrypoint
- [ ] Docker: `deployments/docker/Dockerfile.orchestrator`
- [ ] Docker Compose: orchestrator service
- [ ] Kubernetes: orchestrator deployment + service
- [ ] Health check endpoint
- [ ] Metrics endpoint (Prometheus)

---

## 8. Testing Strategy

### Unit Tests
- Router provider selection logic
- Cost calculation with commission
- Rate limiter counter logic
- Context manager token counting
- Tool registry filtering

### Integration Tests
- Full chat flow: user message → LLM → tool execution → response
- SSE streaming with multiple events
- Provider fallback when primary fails
- Rate limit enforcement
- Balance deduction
- Conversation history loading

### Load Tests
- Concurrent chat requests (100 users)
- Tool execution parallelism
- NATS throughput (agent communication)
- MongoDB query performance

---

## 9. Deployment Architecture

### Development (Docker Compose)
```yaml
services:
  orchestrator:
    build: ./deployments/docker/Dockerfile.orchestrator
    environment:
      - OPENROUTER_API_KEY=${OPENROUTER_API_KEY}
      - POSTGRES_URL=postgresql://postgres:postgres@postgres:5432/onevoice
      - MONGODB_URL=mongodb://mongo:27017/onevoice
      - REDIS_URL=redis://redis:6379
      - NATS_URL=nats://nats:4222
    ports:
      - "8080:8080"
    depends_on:
      - postgres
      - mongo
      - redis
      - nats
```

### Production (Kubernetes)
- **Deployment:** 3 replicas (horizontal scaling)
- **Service:** ClusterIP (internal) + Ingress (external HTTPS)
- **Config:** ConfigMap (llm.yaml) + Secrets (API keys)
- **Resources:** 1 CPU, 2GB RAM per pod
- **Health:** `/health` liveness + readiness probes
- **Monitoring:** Prometheus scraping `/metrics`

---

## 10. Security Considerations

### API Key Management
- All provider API keys in Kubernetes Secrets (not ConfigMaps)
- Rotate keys quarterly
- Monitor for leaked keys (GitHub secret scanning)

### Rate Limiting
- Per-user limits enforced before LLM call
- Per-provider limits to avoid ban (respect ToS)
- Global rate limit at ingress (DDoS protection)

### Cost Controls
- Max daily spend per user (tier-based)
- Negative balance grace period: -$1.00
- Alert when balance < $5.00
- Admin dashboard for cost monitoring

### Data Privacy
- Conversation data encrypted at rest (MongoDB encryption)
- PII redaction in logs
- GDPR compliance: user data export/deletion

---

## 11. Monitoring & Observability

### Metrics (Prometheus)
- `llm_requests_total{model, provider, status}` — Request count
- `llm_request_duration_seconds{model, provider}` — Latency histogram
- `llm_cost_usd{user_id, model, provider}` — Cost gauge
- `llm_tokens_total{user_id, type}` — Token usage counter
- `agent_tool_executions_total{agent, tool, status}` — Tool execution count
- `conversation_active_total` — Active conversations gauge

### Logs (Structured JSON)
```json
{
  "level": "info",
  "timestamp": "2026-02-09T14:30:00Z",
  "service": "orchestrator",
  "user_id": "uuid",
  "conversation_id": "uuid",
  "event": "llm_request",
  "model": "claude-3.5-sonnet-20241022",
  "provider": "openrouter",
  "tokens": {"input": 1200, "output": 350},
  "cost": {"provider": 0.045, "commission": 0.009, "user": 0.054},
  "latency_ms": 1850
}
```

### Traces (OpenTelemetry)
- Span: HTTP request → Orchestrator → LLM Router → Provider API
- Span: Tool execution → NATS publish → Agent handler → Platform API
- Correlation ID through all layers

### Dashboards (Grafana)
- LLM usage (requests/min, cost/hour, tokens/day)
- Provider health (success rate, latency p95, availability)
- Agent performance (tool execution time, failure rate)
- User activity (conversations, messages, tool usage)

---

## 12. Future Enhancements

### Phase 2+ (Not in Initial MVP)
- [ ] LLM response caching (reduce costs for similar queries)
- [ ] Multi-turn streaming (show intermediate LLM thoughts)
- [ ] Custom model fine-tuning (business-specific)
- [ ] Voice input/output (Whisper + TTS)
- [ ] Multi-language support (currently Russian only)
- [ ] A/B testing different prompts
- [ ] Conversation branching (undo/redo)
- [ ] Collaborative conversations (multi-user)

---

## 13. Known Limitations

1. **Context window:** 120K tokens max (truncate or summarize older messages)
2. **Rate limits:** Provider-specific (OpenRouter: flexible, OpenAI: 3.5M tok/min)
3. **Tool execution timeout:** 30s per tool (long-running tasks may fail)
4. **Parallel tool execution:** Max 10 tools simultaneously (NATS concurrency)
5. **SSE compatibility:** Requires HTTP/1.1 (not HTTP/2 server push)
6. **Token counting:** Approximation via tiktoken (may differ from provider)

---

## 14. Dependencies

### Go Modules
```
github.com/sashabaranov/go-openai v1.20.0
github.com/anthropics/anthropic-sdk-go v0.1.0
github.com/go-redis/redis/v9 v9.5.0
github.com/jackc/pgx/v5 v5.5.0
go.mongodb.org/mongo-driver/v2 v2.0.0
github.com/nats-io/nats.go v1.31.0
github.com/pkoukk/tiktoken-go v0.1.6
github.com/go-chi/chi/v5 v5.0.0
gopkg.in/yaml.v3 v3.0.1
```

### External Services
- OpenRouter API (https://openrouter.ai)
- OpenAI API (https://platform.openai.com)
- Anthropic API (https://console.anthropic.com)
- NATS JetStream (self-hosted)
- PostgreSQL 16+
- MongoDB 7+
- Redis 7+

---

## 15. Success Criteria

**Phase 2 is complete when:**
- [ ] User can chat with AI assistant via web UI
- [ ] LLM automatically routes to optimal provider (cost/speed strategy)
- [ ] Tool calls execute on real agents (Telegram, VK, Google — Phase 4)
- [ ] SSE streaming works (thinking phases, tool execution, content)
- [ ] Rate limits enforced (tier-based)
- [ ] Costs tracked and user balance deducted
- [ ] Commission applied and revenue tracked
- [ ] Context window managed (summarization if needed)
- [ ] All tests pass (unit + integration)
- [ ] Service deploys to Kubernetes
- [ ] Monitoring dashboards operational

---

**Document End**

This summary document serves as the complete reference for implementing Phase 2: LLM Orchestrator. All architectural decisions, API specifications, data flows, and implementation details are captured for the development team.
