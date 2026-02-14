# Phase 3: A2A Agent Framework Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Build the A2A (Agent-to-Agent) communication framework so the LLM orchestrator can dispatch tool calls to platform agents (Telegram, VK, Google) over NATS request-reply.

**Architecture:** Shared protocol types live in `pkg/a2a` (imported by both orchestrator and agents). The orchestrator holds a `NATSExecutor` per agent that implements `tools.Executor` — when the LLM requests `telegram__send_post`, the executor JSON-encodes a `ToolRequest`, publishes it on NATS subject `tasks.telegram`, and waits for a `ToolResponse` reply (30s timeout). Platform agents import `pkg/a2a.Agent` base which subscribes to `tasks.{agentID}` and dispatches to a `Handler` interface; Phase 4 agents embed this base.

**Tech Stack:** `github.com/nats-io/nats.go v1.41.1`, `pkg/a2a` (new), `services/orchestrator` (modified), existing `tools.Executor` interface

---

## Working directory

All commands run from: `/Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/`

## Package layout added

```
pkg/a2a/
├── protocol.go          # ToolRequest, ToolResponse types + AgentID constants
├── protocol_test.go
├── agent.go             # Agent base: Subscribe → dispatch → reply
└── agent_test.go

services/orchestrator/internal/natsexec/
├── executor.go          # NATSExecutor implements tools.Executor
└── executor_test.go
```

---

## Task 1: `pkg/a2a` — Protocol types

**Files:**
- Modify: `pkg/go.mod` (add nats.go dependency)
- Create: `pkg/a2a/protocol.go`
- Create: `pkg/a2a/protocol_test.go`

**Context:** `pkg` is at `github.com/f1xgun/onevoice/pkg`. Both orchestrator and future platform agents import `pkg/a2a`. The protocol must be serialisable to JSON (NATS messages are `[]byte`).

### Step 1: Add nats.go to pkg

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && \
  go get github.com/nats-io/nats.go@v1.41.1 && \
  go mod tidy
```

Expected: `go: added github.com/nats-io/nats.go v1.41.1`

### Step 2: Write failing tests first

Create `pkg/a2a/protocol_test.go`:

```go
package a2a_test

import (
	"encoding/json"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolRequest_RoundTrip(t *testing.T) {
	req := a2a.ToolRequest{
		TaskID:     "task-123",
		Tool:       "telegram__send_post",
		Args:       map[string]interface{}{"text": "Привет!"},
		BusinessID: "biz-456",
		RequestID:  "req-789",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded a2a.ToolRequest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, req.TaskID, decoded.TaskID)
	assert.Equal(t, req.Tool, decoded.Tool)
	assert.Equal(t, req.BusinessID, decoded.BusinessID)
	assert.Equal(t, "Привет!", decoded.Args["text"])
}

func TestToolResponse_SuccessRoundTrip(t *testing.T) {
	resp := a2a.ToolResponse{
		TaskID:  "task-123",
		Success: true,
		Result:  map[string]interface{}{"post_id": "12345"},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded a2a.ToolResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.True(t, decoded.Success)
	assert.Equal(t, "task-123", decoded.TaskID)
	assert.Equal(t, "12345", decoded.Result["post_id"])
}

func TestToolResponse_ErrorRoundTrip(t *testing.T) {
	resp := a2a.ToolResponse{
		TaskID:  "task-123",
		Success: false,
		Error:   "platform unavailable",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded a2a.ToolResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.False(t, decoded.Success)
	assert.Equal(t, "platform unavailable", decoded.Error)
}

func TestSubject(t *testing.T) {
	assert.Equal(t, "tasks.telegram", a2a.Subject(a2a.AgentTelegram))
	assert.Equal(t, "tasks.vk", a2a.Subject(a2a.AgentVK))
	assert.Equal(t, "tasks.google", a2a.Subject(a2a.AgentGoogle))
}
```

### Step 3: Verify RED

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && go test ./a2a/...`
Expected: FAIL — `a2a` package does not exist

### Step 4: Implement `pkg/a2a/protocol.go`

```go
package a2a

import "fmt"

// AgentID is the canonical identifier for a platform agent.
type AgentID = string

const (
	AgentTelegram AgentID = "telegram"
	AgentVK       AgentID = "vk"
	AgentGoogle   AgentID = "google"
)

// Subject returns the NATS subject for sending tasks to an agent.
// Pattern: tasks.{agentID}
func Subject(agentID AgentID) string {
	return fmt.Sprintf("tasks.%s", agentID)
}

// ToolRequest is sent from the orchestrator to an agent over NATS.
type ToolRequest struct {
	TaskID     string                 `json:"task_id"`
	Tool       string                 `json:"tool"`        // e.g., "telegram__send_post"
	Args       map[string]interface{} `json:"args"`
	BusinessID string                 `json:"business_id"`
	RequestID  string                 `json:"request_id,omitempty"` // for tracing
}

// ToolResponse is sent back from the agent to the orchestrator.
type ToolResponse struct {
	TaskID  string                 `json:"task_id"`
	Success bool                   `json:"success"`
	Result  map[string]interface{} `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
}
```

### Step 5: Verify GREEN

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && go test ./a2a/...`
Expected: PASS (all 4 tests)

### Step 6: Commit

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator && \
  git add pkg/go.mod pkg/go.sum pkg/a2a/ && \
  git commit -m "feat(a2a): add protocol types and NATS subject helper"
```

---

## Task 2: `pkg/a2a` — Agent base (subscribe + dispatch)

**Files:**
- Create: `pkg/a2a/agent.go`
- Create: `pkg/a2a/agent_test.go`

**Context:** The `Agent` base subscribes to NATS subject `tasks.{agentID}`. For each incoming message it decodes a `ToolRequest`, calls the injected `Handler`, and publishes the `ToolResponse` as the NATS reply. Tests use `nats-server/v2` embedded server via `natsserver "github.com/nats-io/nats-server/v2/server"` — but to keep `pkg` lean, tests use `nats.Connect(nats.DefaultURL)` against a mock transport instead. We test with a `testTransport` adapter.

**Important:** To avoid pulling a heavy nats-server test dependency into `pkg`, we test the `Agent` using a fake `Transport` interface instead of a real NATS connection. The real `nats.Conn` is wired in `cmd/main.go`.

### Step 1: Write failing tests first

Create `pkg/a2a/agent_test.go`:

```go
package a2a_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTransport simulates NATS subscription without a real server.
type fakeTransport struct {
	subscribed string
	handler    func(subject, reply string, data []byte)
}

func (f *fakeTransport) Subscribe(subject string, handler func(subject, reply string, data []byte)) error {
	f.subscribed = subject
	f.handler = handler
	return nil
}

func (f *fakeTransport) Publish(subject string, data []byte) error { return nil }
func (f *fakeTransport) Close()                                     {}

// Trigger simulates receiving a NATS message (test helper).
func (f *fakeTransport) Trigger(subject, reply string, data []byte) {
	if f.handler != nil {
		f.handler(subject, reply, data)
	}
}

func TestAgent_DispatchesToHandler(t *testing.T) {
	transport := &fakeTransport{}
	called := false

	handler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		called = true
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"ok": true},
		}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	assert.Equal(t, "tasks.telegram", transport.subscribed)

	// Simulate an incoming tool request
	req := a2a.ToolRequest{TaskID: "t1", Tool: "telegram__send_post", Args: map[string]interface{}{}}
	data, _ := json.Marshal(req)

	var replyData []byte
	transport.Publish = func(subject string, d []byte) error {
		replyData = d
		return nil
	}
	transport.Trigger("tasks.telegram", "_INBOX.test", data)

	// Give handler goroutine a moment
	time.Sleep(10 * time.Millisecond)

	assert.True(t, called)
	require.NotNil(t, replyData)
	var resp a2a.ToolResponse
	require.NoError(t, json.Unmarshal(replyData, &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "t1", resp.TaskID)
}

func TestAgent_HandlerError_ReturnsErrorResponse(t *testing.T) {
	transport := &fakeTransport{}

	handler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, fmt.Errorf("platform down")
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	req := a2a.ToolRequest{TaskID: "t2", Tool: "telegram__send_post"}
	data, _ := json.Marshal(req)

	var replyData []byte
	transport.Publish = func(_ string, d []byte) error {
		replyData = d
		return nil
	}
	transport.Trigger("tasks.telegram", "_INBOX.test", data)
	time.Sleep(10 * time.Millisecond)

	require.NotNil(t, replyData)
	var resp a2a.ToolResponse
	require.NoError(t, json.Unmarshal(replyData, &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "platform down")
}
```

### Step 2: Verify RED

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && go test ./a2a/...`
Expected: FAIL — `Agent`, `NewAgent`, `HandlerFunc` not defined; also `fmt` not imported in test

(Fix the test — add `"fmt"` to imports before verifying RED.)

### Step 3: Implement `pkg/a2a/agent.go`

```go
package a2a

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Transport abstracts the NATS connection for testability.
type Transport interface {
	Subscribe(subject string, handler func(subject, reply string, data []byte)) error
	Publish(subject string, data []byte) error
	Close()
}

// Handler processes an incoming ToolRequest and returns a ToolResponse.
type Handler interface {
	Handle(ctx context.Context, req ToolRequest) (*ToolResponse, error)
}

// HandlerFunc is a function adapter for Handler.
type HandlerFunc func(ctx context.Context, req ToolRequest) (*ToolResponse, error)

func (f HandlerFunc) Handle(ctx context.Context, req ToolRequest) (*ToolResponse, error) {
	return f(ctx, req)
}

// Agent is the base for all platform agents.
// It subscribes to NATS and dispatches incoming ToolRequests to a Handler.
type Agent struct {
	id        AgentID
	transport Transport
	handler   Handler
}

// NewAgent creates a new Agent.
func NewAgent(id AgentID, transport Transport, handler Handler) *Agent {
	return &Agent{id: id, transport: transport, handler: handler}
}

// Start subscribes to the agent's NATS subject and begins processing requests.
// It returns immediately; processing happens in goroutines spawned per message.
func (a *Agent) Start(ctx context.Context) error {
	subject := Subject(a.id)
	return a.transport.Subscribe(subject, func(subj, reply string, data []byte) {
		go a.handle(ctx, reply, data)
	})
}

func (a *Agent) handle(ctx context.Context, reply string, data []byte) {
	var req ToolRequest
	if err := json.Unmarshal(data, &req); err != nil {
		slog.Error("a2a: failed to decode tool request", "agent", a.id, "error", err)
		return
	}

	resp, err := a.handler.Handle(ctx, req)
	if err != nil {
		resp = &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   err.Error(),
		}
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		slog.Error("a2a: failed to encode tool response", "agent", a.id, "error", err)
		return
	}

	if reply != "" {
		if err := a.transport.Publish(reply, respData); err != nil {
			slog.Error("a2a: failed to publish reply", "agent", a.id, "error", err)
		}
	}
}
```

### Step 4: Fix test — add `"fmt"` import

The `agent_test.go` uses `fmt.Errorf` — ensure `"fmt"` is in imports. Update the file if missing.

### Step 5: Verify GREEN

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && go test -race ./a2a/...`
Expected: PASS (all 6 tests across protocol_test.go + agent_test.go)

### Step 6: Commit

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator && \
  git add pkg/a2a/agent.go pkg/a2a/agent_test.go && \
  git commit -m "feat(a2a): add Agent base with Transport interface and Handler dispatch"
```

---

## Task 3: NATS Executor in orchestrator

**Files:**
- Create: `services/orchestrator/internal/natsexec/executor.go`
- Create: `services/orchestrator/internal/natsexec/executor_test.go`
- Modify: `services/orchestrator/go.mod` (add `nats.go` + `pkg/a2a` dependency)

**Context:** `NATSExecutor` implements `tools.Executor`. It is constructed with a NATS connection, an `agentID` (determines the subject), and a timeout. When `Execute` is called, it:
1. Wraps args into `a2a.ToolRequest` (generates UUID task_id)
2. JSON-encodes and sends via NATS request-reply (`nc.RequestMsgWithContext`)
3. Decodes the `a2a.ToolResponse` reply
4. Returns `resp.Result` on success, or an error on `resp.Success == false`

Tests use the same `fakeTransport` style — but since `NATSExecutor` takes a `*nats.Conn`, we need an interface adapter. Define a `Requester` interface so tests can inject a fake.

### Step 1: Add dependencies to orchestrator go.mod

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && \
  go get github.com/nats-io/nats.go@v1.41.1 && \
  go get github.com/google/uuid && \
  go mod tidy
```

### Step 2: Write failing tests first

Create `services/orchestrator/internal/natsexec/executor_test.go`:

```go
package natsexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRequester simulates NATS request-reply without a real server.
type fakeRequester struct {
	response *a2a.ToolResponse
	err      error
}

func (f *fakeRequester) Request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	out, _ := json.Marshal(f.response)
	return out, nil
}

func TestNATSExecutor_SuccessfulExecution(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t1",
			Success: true,
			Result:  map[string]interface{}{"post_id": "999"},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, fake)
	result, err := exec.Execute(context.Background(), map[string]interface{}{
		"text": "Hello World",
	})

	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "999", m["post_id"])
}

func TestNATSExecutor_AgentReturnsError(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t2",
			Success: false,
			Error:   "rate limit exceeded",
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestNATSExecutor_TransportError(t *testing.T) {
	fake := &fakeRequester{err: context.DeadlineExceeded}
	exec := natsexec.New(a2a.AgentTelegram, fake)

	_, err := exec.Execute(context.Background(), nil)
	require.Error(t, err)
}
```

### Step 3: Verify RED

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && go test ./internal/natsexec/...`
Expected: FAIL — package does not exist

### Step 4: Implement `services/orchestrator/internal/natsexec/executor.go`

```go
package natsexec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/google/uuid"
)

// Requester abstracts NATS request-reply for testability.
// The real implementation wraps *nats.Conn.
type Requester interface {
	Request(ctx context.Context, subject string, data []byte) ([]byte, error)
}

// NATSExecutor implements tools.Executor by sending tool requests
// to a platform agent via NATS request-reply.
type NATSExecutor struct {
	agentID a2a.AgentID
	req     Requester
}

// New creates a NATSExecutor for the given agent.
func New(agentID a2a.AgentID, requester Requester) *NATSExecutor {
	return &NATSExecutor{agentID: agentID, req: requester}
}

// Execute sends a ToolRequest to the agent and returns its result.
// It implements tools.Executor.
func (e *NATSExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := a2a.ToolRequest{
		TaskID: uuid.New().String(),
		Tool:   e.agentID, // will be overridden by registry — callers set tool name separately
		Args:   args,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("natsexec: marshal request: %w", err)
	}

	subject := a2a.Subject(e.agentID)
	replyData, err := e.req.Request(ctx, subject, data)
	if err != nil {
		return nil, fmt.Errorf("natsexec: request to %s: %w", subject, err)
	}

	var resp a2a.ToolResponse
	if err := json.Unmarshal(replyData, &resp); err != nil {
		return nil, fmt.Errorf("natsexec: decode response: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("natsexec: agent %s error: %s", e.agentID, resp.Error)
	}

	return resp.Result, nil
}
```

### Step 5: Verify GREEN

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && go test -race ./internal/natsexec/...`
Expected: PASS (all 3 tests)

### Step 6: Commit

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator && \
  git add services/orchestrator/go.mod services/orchestrator/go.sum \
          services/orchestrator/internal/natsexec/ && \
  git commit -m "feat(orchestrator): add NATS executor implementing tools.Executor"
```

---

## Task 4: Wire NATS into orchestrator config + main.go

**Files:**
- Modify: `services/orchestrator/internal/config/config.go` (add `NATSUrl`)
- Modify: `services/orchestrator/internal/config/config_test.go` (add test for NATS URL default)
- Modify: `services/orchestrator/cmd/main.go` (connect to NATS, register NATSExecutors)

**Context:** `NATS_URL` defaults to `nats://localhost:4222`. If NATS is unavailable at startup, the orchestrator logs a warning and continues without NATS (graceful degradation — tool calls will fall back to stubs). The `NATSConn` adapter wraps `*nats.Conn` to implement `natsexec.Requester`.

### Step 1: Update config

Read `services/orchestrator/internal/config/config.go`. Add `NATSUrl string` field and read from `NATS_URL` env var (default `nats://localhost:4222`).

Updated `Load()`:
```go
return &Config{
    Port:          getEnv("PORT", "8090"),
    LLMModel:      model,
    LLMTier:       getEnv("LLM_TIER", "free"),
    MaxIterations: maxIter,
    NATSUrl:       getEnv("NATS_URL", "nats://localhost:4222"),
}, nil
```

Updated struct:
```go
type Config struct {
    Port          string
    LLMModel      string
    LLMTier       string
    MaxIterations int
    NATSUrl       string
}
```

### Step 2: Update config test

Add one test to `config_test.go`:

```go
func TestLoad_DefaultNATSUrl(t *testing.T) {
    t.Setenv("LLM_MODEL", "gpt-4o-mini")
    cfg, err := config.Load()
    require.NoError(t, err)
    assert.Equal(t, "nats://localhost:4222", cfg.NATSUrl)
}
```

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && go test ./internal/config/...`
Expected: PASS (4 tests now)

### Step 3: Add NATSConn adapter in natsexec

Add `nats_conn.go` to the natsexec package (NOT tested — it's a thin wrapper over the real nats library):

Create `services/orchestrator/internal/natsexec/nats_conn.go`:

```go
package natsexec

import (
	"context"

	natslib "github.com/nats-io/nats.go"
)

// NATSConn wraps *nats.Conn to implement Requester.
type NATSConn struct {
	nc *natslib.Conn
}

// NewNATSConn wraps an existing *nats.Conn.
func NewNATSConn(nc *natslib.Conn) *NATSConn {
	return &NATSConn{nc: nc}
}

// Request sends data to subject and waits for a reply (uses context for timeout).
func (c *NATSConn) Request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	msg, err := c.nc.RequestMsgWithContext(ctx, &natslib.Msg{
		Subject: subject,
		Data:    data,
	})
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}
```

### Step 4: Update main.go

Read `services/orchestrator/cmd/main.go`. Replace the current static tool registry section with NATS-connected executors. If NATS connection fails, log a warning and continue with empty tool registry.

Replace main.go content:

```go
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	log := logger.New("orchestrator")
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Build LLM registry
	registry := llm.NewRegistry()
	registry.RegisterModelProvider(&llm.ModelProviderEntry{
		Model:        cfg.LLMModel,
		Provider:     "stub",
		HealthStatus: "healthy",
		Enabled:      true,
	})
	router := llm.NewRouter(registry)

	// Tool registry — register NATS executors if NATS is available
	toolRegistry := tools.NewRegistry()
	nc, natsErr := natslib.Connect(cfg.NATSUrl)
	if natsErr != nil {
		log.Warn("NATS unavailable — tools will return stubs", "url", cfg.NATSUrl, "error", natsErr)
	} else {
		defer nc.Close()
		log.Info("connected to NATS", "url", cfg.NATSUrl)
		registerPlatformTools(toolRegistry, nc)
	}

	// Business context
	biz := prompt.BusinessContext{
		Name: os.Getenv("BUSINESS_NAME"),
		Now:  time.Now(),
	}

	// Orchestrator
	orch := orchestrator.NewWithOptions(router, toolRegistry, orchestrator.Options{
		MaxIterations: cfg.MaxIterations,
	})

	// HTTP handler
	chatHandler := handler.NewChatHandler(orch, biz)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Post("/chat/{conversationID}", chatHandler.Chat)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := ":" + cfg.Port
	log.Info("orchestrator listening", "addr", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

// registerPlatformTools wires NATS executors into the tool registry for each agent.
func registerPlatformTools(reg *tools.Registry, nc *natslib.Conn) {
	agents := []struct {
		id    a2a.AgentID
		tools []llm.ToolDefinition
	}{
		{
			id: a2a.AgentTelegram,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "telegram__send_post",
					Description: "Публикует сообщение в Telegram-канал",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text":       map[string]interface{}{"type": "string", "description": "Текст сообщения"},
							"channel_id": map[string]interface{}{"type": "string", "description": "ID канала"},
						},
						"required": []string{"text"},
					},
				}},
			},
		},
		{
			id: a2a.AgentVK,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "vk__publish_post",
					Description: "Публикует пост в сообщество ВКонтакте",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text":     map[string]interface{}{"type": "string", "description": "Текст поста"},
							"group_id": map[string]interface{}{"type": "string", "description": "ID сообщества"},
						},
						"required": []string{"text"},
					},
				}},
			},
		},
		{
			id: a2a.AgentGoogle,
			tools: []llm.ToolDefinition{
				{Type: "function", Function: llm.FunctionDefinition{
					Name:        "google__update_hours",
					Description: "Обновляет часы работы в Google Business Profile",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"hours":       map[string]interface{}{"type": "string", "description": "Часы работы в формате JSON"},
							"location_id": map[string]interface{}{"type": "string", "description": "ID локации"},
						},
						"required": []string{"hours"},
					},
				}},
			},
		},
	}

	conn := natsexec.NewNATSConn(nc)
	for _, a := range agents {
		exec := natsexec.New(a.id, conn)
		for _, def := range a.tools {
			reg.Register(def, exec)
		}
	}
}
```

### Step 5: Verify all tests + build

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && go test -race ./...`
Expected: PASS (all packages, including new config test)

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/services/orchestrator && go build ./cmd/...`
Expected: builds without error

### Step 6: Run full workspace test suite

Run: `cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator/pkg && go test -race ./a2a/... ./llm/... ./llm/providers/...`
Expected: all PASS

### Step 7: Commit

```bash
cd /Users/f1xgun/onevoice/.worktrees/phase2-orchestrator && \
  git add services/orchestrator/internal/config/ \
          services/orchestrator/internal/natsexec/nats_conn.go \
          services/orchestrator/cmd/main.go \
          services/orchestrator/go.mod services/orchestrator/go.sum && \
  git commit -m "feat(orchestrator): wire NATS executors into tool registry, add NATS_URL config"
```

---

## Verification Checklist

After all 4 tasks complete, run from the worktree root:

```bash
# pkg/a2a tests
cd pkg && go test -race ./a2a/... && echo "pkg/a2a OK"

# orchestrator tests
cd services/orchestrator && go test -race ./... && echo "orchestrator OK"

# build
go build ./services/orchestrator/cmd/... && echo "build OK"

# pkg/llm still passes
cd pkg && go test -race ./llm/... ./llm/providers/... && echo "pkg/llm OK"
```

Expected output:
```
ok  github.com/f1xgun/onevoice/pkg/a2a
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/config
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/handler
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/prompt
ok  github.com/f1xgun/onevoice/services/orchestrator/internal/tools
```
