# Self-Hosted LLM Provider — Design

**Date:** 2026-02-15
**Status:** Approved

---

## Problem

The orchestrator currently supports three cloud LLM providers (OpenRouter, OpenAI, Anthropic).
Users running their own LLM inference servers (Ollama, vLLM, LM Studio, llama.cpp server, etc.)
cannot point the orchestrator at a self-hosted endpoint without modifying code.

**Goal:** Support one or more self-hosted, OpenAI-compatible endpoints with reliable function
calling, configured entirely via environment variables — no code changes required.

---

## Constraints

- Self-hosted servers must expose an OpenAI-compatible `/v1/chat/completions` endpoint
- Function calling (tool use) must be supported by the model/server
- Multiple endpoints supported (different models on different VMs)
- API key is optional (many self-hosted servers accept any string or none)
- Implementation must follow existing provider patterns (no special-casing in the router)

---

## Configuration — Option A: Indexed Env Vars

Contiguous indices starting at 0. Scan stops at first missing `_URL`.

```env
SELF_HOSTED_0_URL=http://vm1:11434/v1
SELF_HOSTED_0_MODEL=llama3.1
SELF_HOSTED_0_API_KEY=          # optional; omit or leave empty

SELF_HOSTED_1_URL=http://vm2:8080/v1
SELF_HOSTED_1_MODEL=mistral
SELF_HOSTED_1_API_KEY=sk-internal-token
```

Rules:
- `URL` is required per entry; missing URL at index N stops the scan
- `MODEL` is required per entry; entry without MODEL is skipped with a warning log
- `API_KEY` is optional; if empty, `"none"` is passed to the HTTP client (accepted by most servers)

---

## Architecture

### New file: `pkg/llm/providers/selfhosted.go`

```
SelfHostedProvider
  ├── client  *openai.Client   (go-openai with custom BaseURL)
  └── name    string           ("selfhosted-0", "selfhosted-1", …)

NewSelfHosted(name, baseURL, apiKey string) *SelfHostedProvider
  cfg := openai.DefaultConfig(apiKey)
  cfg.BaseURL = baseURL
  client = openai.NewClientWithConfig(cfg)

Implements llm.Provider interface:
  Name() string
  Chat(ctx, req) (*Response, error)   ← identical logic to OpenAI provider
```

### Config changes: `services/orchestrator/internal/config/config.go`

```
SelfHostedEndpoint { URL, Model, APIKey string }

Config.SelfHostedEndpoints []SelfHostedEndpoint

parseIndexedEndpoints() []SelfHostedEndpoint
  loop i=0,1,2,… while SELF_HOSTED_{i}_URL != ""
    skip entry if SELF_HOSTED_{i}_MODEL == "" (log warning)
    append SelfHostedEndpoint{URL, Model, APIKey}
```

### Wiring: `services/orchestrator/cmd/main.go`

Added to `buildProviderOpts` after existing cloud provider loop:

```
for i, ep := range cfg.SelfHostedEndpoints:
  name = "selfhosted-{i}"
  p = providers.NewSelfHosted(name, ep.URL, ep.APIKey)
  opts += llm.WithProvider(p)
  registry.RegisterModelProvider{Model: ep.Model, Provider: name, healthy, enabled}
  log.Info("self-hosted LLM registered", name, url, model)
```

---

## Data Flow

```
env vars
  SELF_HOSTED_0_URL / MODEL / API_KEY
  SELF_HOSTED_1_URL / MODEL / API_KEY
        │
        ▼
config.Load() → []SelfHostedEndpoint
        │
        ▼
buildProviderOpts() → SelfHostedProvider{go-openai client, custom BaseURL}
                    → registry entry: Model=llama3.1 → Provider=selfhosted-0
        │
        ▼
llm.Router.Chat(model="llama3.1", messages, tools)
  → lookup registry → picks "selfhosted-0"
  → SelfHostedProvider.Chat()
  → POST http://vm1:11434/v1/chat/completions
        │
        ▼
JSON response (OpenAI format) → parsed → llm.Response
```

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| No self-hosted endpoints + no cloud keys | Existing startup error: "no LLM provider API key set" |
| Endpoint configured but server unreachable | First request fails with HTTP error; router marks provider unhealthy |
| `_MODEL` missing for endpoint i | Warning log, endpoint skipped |
| `_URL` missing at index N | Scan stops; indices N+1, N+2,… not checked |
| Duplicate model name across endpoints | Last registration wins (router uses most recent entry) |

No startup connectivity check — consistent with how cloud providers are handled.

---

## Files Changed

| File | Change |
|------|--------|
| `pkg/llm/providers/selfhosted.go` | **New** — provider implementation |
| `pkg/llm/providers/selfhosted_test.go` | **New** — httptest-based unit tests |
| `services/orchestrator/internal/config/config.go` | Add `SelfHostedEndpoint`, `parseIndexedEndpoints` |
| `services/orchestrator/internal/config/config_test.go` | New test cases for indexed endpoint parsing |
| `services/orchestrator/cmd/main.go` | Wire self-hosted loop in `buildProviderOpts` |

---

## Tests

- **`TestSelfHostedProvider_Chat`** — `httptest.Server` returns canned `chat/completions` JSON; assert correct message returned
- **`TestSelfHostedProvider_Name`** — assert `Name()` returns constructor-provided name
- **`TestLoad_SelfHostedEndpoints`** — sets `SELF_HOSTED_0_*` + `SELF_HOSTED_1_*`, asserts parsed slice
- **`TestLoad_SelfHostedEndpoints_MissingModel`** — endpoint with no MODEL is skipped
- **`TestLoad_SelfHostedEndpoints_StopsAtGap`** — index 0 set, index 1 missing, index 2 set → only index 0 returned
