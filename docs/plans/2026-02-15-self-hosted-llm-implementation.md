# Self-Hosted LLM Provider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `SelfHostedProvider` that routes LLM calls to any OpenAI-compatible inference server (Ollama, vLLM, etc.) configured via indexed env vars `SELF_HOSTED_N_URL / _MODEL / _API_KEY`.

**Architecture:** New `pkg/llm/providers/selfhosted.go` wraps `go-openai` with a custom `BaseURL` via `openai.NewClientWithConfig`. Config parsing scans `SELF_HOSTED_0_*`, `SELF_HOSTED_1_*`, … until a missing `_URL`. `buildProviderOpts` in `cmd/main.go` registers each endpoint with a unique provider name `"selfhosted-N"`.

**Tech Stack:** Go 1.22+, `github.com/sashabaranov/go-openai` (already in `pkg/go.mod`), `net/http/httptest` for provider tests.

---

### Task 1: `SelfHostedProvider` struct + `Name()` + constructor

**Files:**
- Create: `pkg/llm/providers/selfhosted.go`
- Create: `pkg/llm/providers/selfhosted_test.go`

**Context:**
All existing providers live in `pkg/llm/providers/`. The `llm.Provider` interface (in `pkg/llm/provider.go`) requires: `Name()`, `Chat()`, `ChatStream()`, `ListModels()`, `HealthCheck()`.

The `go-openai` library supports custom base URLs via:
```go
cfg := openai.DefaultConfig(apiKey) // apiKey can be "none" if empty
cfg.BaseURL = baseURL
client := openai.NewClientWithConfig(cfg)
```

**Step 1: Write the failing test**

Create `pkg/llm/providers/selfhosted_test.go`:
```go
package providers_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/stretchr/testify/assert"
)

func TestSelfHostedProvider_Name(t *testing.T) {
	p := providers.NewSelfHosted("selfhosted-0", "http://localhost:11434/v1", "")
	assert.Equal(t, "selfhosted-0", p.Name())
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/f1xgun/onevoice/pkg && go test ./llm/providers/... -run TestSelfHostedProvider_Name -v
```
Expected: FAIL — `providers.NewSelfHosted` undefined

**Step 3: Write minimal implementation**

Create `pkg/llm/providers/selfhosted.go`:
```go
package providers

import (
	openai "github.com/sashabaranov/go-openai"
)

// SelfHostedProvider implements llm.Provider for any OpenAI-compatible inference server.
type SelfHostedProvider struct {
	client *openai.Client
	name   string
}

// NewSelfHosted creates a provider pointing at baseURL.
// apiKey is optional — pass "" if the server requires no authentication.
// name must be unique (e.g. "selfhosted-0") to distinguish multiple endpoints in the router.
func NewSelfHosted(name, baseURL, apiKey string) *SelfHostedProvider {
	key := apiKey
	if key == "" {
		key = "none" // go-openai requires a non-empty string; most servers ignore it
	}
	cfg := openai.DefaultConfig(key)
	cfg.BaseURL = baseURL
	return &SelfHostedProvider{
		client: openai.NewClientWithConfig(cfg),
		name:   name,
	}
}

// Name returns the unique provider identifier set at construction time.
func (p *SelfHostedProvider) Name() string { return p.name }
```

**Step 4: Run test to verify it passes**

```bash
cd /Users/f1xgun/onevoice/pkg && go test ./llm/providers/... -run TestSelfHostedProvider_Name -v
```
Expected: PASS

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add pkg/llm/providers/selfhosted.go pkg/llm/providers/selfhosted_test.go
git commit -m "feat(llm): add SelfHostedProvider skeleton with Name()"
```

---

### Task 2: `HealthCheck`, `ListModels`, `Chat`, `ChatStream` implementations

**Files:**
- Modify: `pkg/llm/providers/selfhosted.go`
- Modify: `pkg/llm/providers/selfhosted_test.go`

**Context:**
Copy the full `Chat` and `ChatStream` logic from `pkg/llm/providers/openai.go` (lines 59–230) — they are identical except the `Provider` field in the response must be `p.name`, not `"openai"`.

`HealthCheck` and `ListModels` are unreliable on self-hosted servers (not all support `/v1/models`). Implement them as no-ops: `HealthCheck` returns `nil`, `ListModels` returns empty slice.

**Step 1: Write failing test for Chat (httptest server)**

Add to `pkg/llm/providers/selfhosted_test.go`:
```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	// existing imports...
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSelfHostedProvider_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
			"usage":   map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer srv.Close()

	p := providers.NewSelfHosted("selfhosted-0", srv.URL+"/v1", "")
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		UserID:    uuid.New(),
		Model:     "llama3.1",
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", resp.Content)
	assert.Equal(t, "selfhosted-0", resp.Provider)
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/f1xgun/onevoice/pkg && go test ./llm/providers/... -run TestSelfHostedProvider_Chat -v
```
Expected: FAIL — `p.Chat` undefined (method not yet implemented)

**Step 3: Implement all interface methods**

Append to `pkg/llm/providers/selfhosted.go`:
```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	openai "github.com/sashabaranov/go-openai"
)

// HealthCheck always returns nil — self-hosted servers may not support /v1/models.
func (p *SelfHostedProvider) HealthCheck(_ context.Context) error { return nil }

// ListModels returns empty — model discovery is not reliable on self-hosted servers.
func (p *SelfHostedProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}

// Chat sends a chat completion request to the self-hosted server.
func (p *SelfHostedProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()

	msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			oaiToolCalls := make([]openai.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				oaiToolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
			msg.ToolCalls = oaiToolCalls
		}
		msgs[i] = msg
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	}

	if len(req.Tools) > 0 {
		tools := make([]openai.Tool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
		oaiReq.Tools = tools
	}

	resp, err := p.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("selfhosted chat: %w", err)
	}

	var content, finishReason string
	var toolCalls []llm.ToolCall
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		content = choice.Message.Content
		finishReason = string(choice.FinishReason)
		for _, tc := range choice.Message.ToolCalls {
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   tc.ID,
				Type: string(tc.Type),
				Function: llm.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return &llm.ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: llm.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Latency:     time.Since(start),
		RawResponse: resp,
		Provider:    p.name,
	}, nil
}

// ChatStream returns a channel of incremental responses from the self-hosted server.
func (p *SelfHostedProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			oaiToolCalls := make([]openai.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				oaiToolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
			msg.ToolCalls = oaiToolCalls
		}
		msgs[i] = msg
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Stream:      true,
	}

	if len(req.Tools) > 0 {
		tools := make([]openai.Tool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
		oaiReq.Tools = tools
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("selfhosted stream: %w", err)
	}

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			resp, err := stream.Recv()
			if err != nil {
				chunk := llm.StreamChunk{Done: true}
				if !errors.Is(err, io.EOF) {
					chunk.Error = err
				}
				select {
				case ch <- chunk:
				case <-ctx.Done():
				}
				return
			}
			delta := ""
			if len(resp.Choices) > 0 {
				delta = resp.Choices[0].Delta.Content
			}
			select {
			case ch <- llm.StreamChunk{Delta: delta}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
```

**Note:** The import block at the top of the file needs to be merged — combine the initial `openai` import with the new ones. Final imports:
```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	openai "github.com/sashabaranov/go-openai"
)
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/f1xgun/onevoice/pkg && go test ./llm/providers/... -run TestSelfHostedProvider -v
```
Expected: PASS for both `TestSelfHostedProvider_Name` and `TestSelfHostedProvider_Chat`

Also run full provider suite to ensure no regressions:
```bash
cd /Users/f1xgun/onevoice/pkg && go test -race ./llm/providers/...
```
Expected: PASS (live tests skipped automatically if env vars absent)

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add pkg/llm/providers/selfhosted.go pkg/llm/providers/selfhosted_test.go
git commit -m "feat(llm): implement SelfHostedProvider Chat/ChatStream/HealthCheck/ListModels"
```

---

### Task 3: Config — `SelfHostedEndpoint` struct + `parseIndexedEndpoints`

**Files:**
- Modify: `services/orchestrator/internal/config/config.go`
- Modify: `services/orchestrator/internal/config/config_test.go`

**Context:**
Current `config.go` already has `parseCSV` helper and `getEnv`. Add `SelfHostedEndpoint` struct, a new field on `Config`, and the `parseIndexedEndpoints` function. Then call it in `Load()`.

**Step 1: Write failing tests**

Add to `services/orchestrator/internal/config/config_test.go`:
```go
func TestLoad_SelfHostedEndpoints(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	t.Setenv("SELF_HOSTED_0_API_KEY", "sk-local")
	t.Setenv("SELF_HOSTED_1_URL", "http://vm2:8080/v1")
	t.Setenv("SELF_HOSTED_1_MODEL", "mistral")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 2)
	assert.Equal(t, "http://vm1:11434/v1", cfg.SelfHostedEndpoints[0].URL)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
	assert.Equal(t, "sk-local", cfg.SelfHostedEndpoints[0].APIKey)
	assert.Equal(t, "http://vm2:8080/v1", cfg.SelfHostedEndpoints[1].URL)
	assert.Equal(t, "mistral", cfg.SelfHostedEndpoints[1].Model)
	assert.Empty(t, cfg.SelfHostedEndpoints[1].APIKey)
}

func TestLoad_SelfHostedEndpoints_MissingModel_Skipped(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	// no SELF_HOSTED_0_MODEL

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SelfHostedEndpoints)
}

func TestLoad_SelfHostedEndpoints_StopsAtGap(t *testing.T) {
	t.Setenv("LLM_MODEL", "gpt-4o-mini")
	t.Setenv("SELF_HOSTED_0_URL", "http://vm1:11434/v1")
	t.Setenv("SELF_HOSTED_0_MODEL", "llama3.1")
	// index 1 missing — scan stops here
	t.Setenv("SELF_HOSTED_2_URL", "http://vm3:11434/v1")
	t.Setenv("SELF_HOSTED_2_MODEL", "gemma")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Len(t, cfg.SelfHostedEndpoints, 1)
	assert.Equal(t, "llama3.1", cfg.SelfHostedEndpoints[0].Model)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/f1xgun/onevoice/services/orchestrator && go test ./internal/config/... -run TestLoad_SelfHosted -v
```
Expected: FAIL — `cfg.SelfHostedEndpoints` undefined

**Step 3: Implement struct + parsing**

In `services/orchestrator/internal/config/config.go`, add after the `Config` struct:

```go
// SelfHostedEndpoint holds the config for one self-hosted LLM endpoint.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}
```

Add `SelfHostedEndpoints []SelfHostedEndpoint` to `Config`:
```go
type Config struct {
	// ... existing fields (Port, LLMModel, LLMTier, MaxIterations, NATSUrl,
	//     OpenRouterAPIKey, OpenAIAPIKey, AnthropicAPIKey,
	//     BusinessName, BusinessCategory, BusinessTone, ActiveIntegrations) ...
	SelfHostedEndpoints []SelfHostedEndpoint
}
```

Add `parseIndexedEndpoints` function (place it after `parseCSV`):
```go
// parseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY env vars
// for N = 0, 1, 2, … until a missing _URL. Entries without _MODEL are skipped.
func parseIndexedEndpoints() []SelfHostedEndpoint {
	var result []SelfHostedEndpoint
	for i := 0; ; i++ {
		prefix := fmt.Sprintf("SELF_HOSTED_%d_", i)
		url := os.Getenv(prefix + "URL")
		if url == "" {
			break
		}
		model := os.Getenv(prefix + "MODEL")
		if model == "" {
			continue // skip entry without model; log is in caller
		}
		result = append(result, SelfHostedEndpoint{
			URL:    url,
			Model:  model,
			APIKey: os.Getenv(prefix + "API_KEY"),
		})
	}
	return result
}
```

In `Load()`, add the call at the end of the return block:
```go
return &Config{
	// ... existing fields ...
	SelfHostedEndpoints: parseIndexedEndpoints(),
}, nil
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/f1xgun/onevoice/services/orchestrator && go test -race ./internal/config/... -v
```
Expected: all tests PASS (existing + 3 new)

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/orchestrator/internal/config/config.go \
        services/orchestrator/internal/config/config_test.go
git commit -m "feat(config): add SelfHostedEndpoint and parseIndexedEndpoints"
```

---

### Task 4: Wire self-hosted providers in `cmd/main.go`

**Files:**
- Modify: `services/orchestrator/cmd/main.go`

**Context:**
`buildProviderOpts` (in `cmd/main.go`) already loops over cloud providers. Add a second loop for `cfg.SelfHostedEndpoints`. Each endpoint gets a unique name `"selfhosted-N"`.

There are no new tests for `main.go` (it's a wiring function without testable unit logic; integration is covered by the provider test). Instead we verify the build compiles and the existing orchestrator tests still pass.

**Step 1: Add the import for `providers` package (already imported)**

Confirm `services/orchestrator/cmd/main.go` imports:
```go
"github.com/f1xgun/onevoice/pkg/llm/providers"
```
(Already present — no change needed.)

**Step 2: Add the self-hosted loop in `buildProviderOpts`**

In `buildProviderOpts`, after the existing `specs` loop, add:

```go
	// Wire self-hosted endpoints
	for i, ep := range cfg.SelfHostedEndpoints {
		name := fmt.Sprintf("selfhosted-%d", i)
		p := providers.NewSelfHosted(name, ep.URL, ep.APIKey)
		opts = append(opts, llm.WithProvider(p))
		reg.RegisterModelProvider(&llm.ModelProviderEntry{
			Model:        ep.Model,
			Provider:     name,
			HealthStatus: "healthy",
			Enabled:      true,
		})
		log.Info("self-hosted LLM registered", "name", name, "url", ep.URL, "model", ep.Model)
	}
```

The function signature is already `buildProviderOpts(cfg *config.Config, reg *llm.Registry, log *slog.Logger) []llm.RouterOption` — the `cfg` already has `SelfHostedEndpoints` after Task 3.

Also add `"fmt"` to the import block if it's not already there (check — it was not imported in the original main.go). Add it:
```go
import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
	// ... rest unchanged ...
)
```

**Step 3: Build to verify it compiles**

```bash
cd /Users/f1xgun/onevoice/services/orchestrator && go build -o /tmp/orchestrator-selfhosted ./cmd/...
```
Expected: success, binary at `/tmp/orchestrator-selfhosted`

**Step 4: Run orchestrator tests**

```bash
cd /Users/f1xgun/onevoice/services/orchestrator && go test -race ./...
```
Expected: PASS

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/orchestrator/cmd/main.go
git commit -m "feat(orchestrator): wire self-hosted LLM endpoints from config"
```

---

### Task 5: Compliance check + final test run

**Files:** none (verification only)

**Step 1: Run full pkg/llm/providers suite including compliance test**

```bash
cd /Users/f1xgun/onevoice/pkg && go test -race ./llm/providers/... -v
```
Expected: PASS. The `compliance_test.go` checks all registered providers satisfy the interface — `SelfHostedProvider` must appear there if the test iterates over a provider list. Check `compliance_test.go` first:

```bash
cat pkg/llm/providers/compliance_test.go
```

If `compliance_test.go` builds a list of providers to test, add a `NewSelfHosted("selfhosted-0", "http://localhost:1", "")` entry to it. If it only tests named providers, no change needed.

**Step 2: Run workspace-wide race detector**

```bash
cd /Users/f1xgun/onevoice && go test -race ./pkg/llm/... ./services/orchestrator/...
```
Expected: PASS

**Step 3: Commit (if compliance_test.go was modified)**

```bash
cd /Users/f1xgun/onevoice
git add pkg/llm/providers/compliance_test.go  # only if modified
git commit -m "test(llm): add SelfHostedProvider to compliance test suite"
```

If compliance_test.go needed no changes, skip this commit.
