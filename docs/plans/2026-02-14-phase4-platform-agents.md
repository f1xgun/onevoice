# Phase 4: Platform Agents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Build 3 standalone platform agent services (Telegram API, VK API, Yandex.Business RPA) that each embed `pkg/a2a.Agent`, subscribe to NATS, and execute real platform operations.

**Architecture:** Each agent is a standalone Go service with its own `go.mod`. It imports `pkg/a2a` and wires a `NATSTransport` adapter to a platform-specific `Handler`. The handler routes `ToolRequest.Tool` to the correct platform function. The Yandex.Business agent additionally uses `playwright-go` for RPA (no public API available).

**MVP platforms (per §2 Approved Decisions):**
- Telegram (API, Bot Token) — `tasks.telegram`
- VK (API, OAuth 2.0) — `tasks.vk`
- Yandex.Business (RPA, Playwright + Yandex ID cookie) — `tasks.yandex_business`

**Google Business Profile**: NOT MVP — excluded per Decision #6 (only 3% map usage in Russia).

**Tech Stack:** `github.com/nats-io/nats.go v1.41.1`, `github.com/go-telegram-bot-api/telegram-bot-api/v5`, `github.com/SevereCloud/vksdk/v3`, `github.com/playwright-community/playwright-go`

---

## Working directory

All commands run from: `/Users/f1xgun/onevoice/`

## New services structure

```
services/
├── agent-telegram/
│   ├── go.mod          # module github.com/f1xgun/onevoice/services/agent-telegram
│   ├── cmd/main.go
│   └── internal/
│       ├── agent/      # Handler implementation
│       └── telegram/   # Telegram Bot API client wrapper
│
├── agent-vk/
│   ├── go.mod
│   ├── cmd/main.go
│   └── internal/
│       ├── agent/
│       └── vk/
│
└── agent-yandex-business/
    ├── go.mod
    ├── cmd/main.go
    └── internal/
        ├── agent/
        └── yandex/     # Playwright-based automation
```

---

## Task 1: NATS Transport adapter in `pkg/a2a`

**Files:**
- Create: `pkg/a2a/nats_transport.go`
- Create: `pkg/a2a/nats_transport_test.go` (fake NATS)

**Context:** The `Agent.Start()` takes a `Transport` interface. Platform agents need a real NATS-backed `Transport`. The `NATSTransport` wraps `*nats.Conn` and adapts its subscription/publish API to the `Transport` interface.

### Step 1: Write failing tests first

Create `pkg/a2a/nats_transport_test.go`:

```go
package a2a_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/stretchr/testify/assert"
)

// Compile-time check that NATSTransport implements Transport.
var _ a2a.Transport = (*a2a.NATSTransport)(nil)

func TestNATSTransport_ImplementsTransport(t *testing.T) {
	// This test verifies NATSTransport satisfies the Transport interface.
	// Real NATS connection is not required — the compile-time check above is sufficient.
	assert.NotNil(t, new(a2a.NATSTransport))
}
```

Run: `cd /Users/f1xgun/onevoice/pkg && go test ./a2a/...`
Expected: FAIL — `NATSTransport` not defined

### Step 2: Implement `pkg/a2a/nats_transport.go`

```go
package a2a

import (
	natslib "github.com/nats-io/nats.go"
)

// NATSTransport adapts *nats.Conn to the Transport interface.
type NATSTransport struct {
	nc *natslib.Conn
}

// NewNATSTransport wraps an existing *nats.Conn.
func NewNATSTransport(nc *natslib.Conn) *NATSTransport {
	return &NATSTransport{nc: nc}
}

// Subscribe registers a message handler on the given NATS subject.
func (t *NATSTransport) Subscribe(subject string, handler func(subject, reply string, data []byte)) error {
	_, err := t.nc.Subscribe(subject, func(msg *natslib.Msg) {
		handler(msg.Subject, msg.Reply, msg.Data)
	})
	return err
}

// Publish sends data to a NATS subject (used for replying to requests).
func (t *NATSTransport) Publish(subject string, data []byte) error {
	return t.nc.Publish(subject, data)
}

// Close drains and closes the NATS connection.
func (t *NATSTransport) Close() {
	t.nc.Drain()
}
```

### Step 3: Verify GREEN

Run: `cd /Users/f1xgun/onevoice/pkg && go test -race ./a2a/...`
Expected: PASS (all tests)

### Step 4: Update `go.work` for new agent modules

Append to `go.work` after `services/orchestrator`:

```
./services/agent-telegram
./services/agent-vk
./services/agent-yandex-business
```

### Step 5: Commit

```bash
cd /Users/f1xgun/onevoice && \
  git add pkg/a2a/nats_transport.go pkg/a2a/nats_transport_test.go go.work && \
  git commit -m "feat(a2a): add NATSTransport adapter for platform agents"
```

---

## Task 2: Telegram Agent service scaffold + handler

**Files:**
- Create: `services/agent-telegram/go.mod`
- Create: `services/agent-telegram/internal/agent/handler.go`
- Create: `services/agent-telegram/internal/agent/handler_test.go`
- Create: `services/agent-telegram/cmd/main.go`

**Context:** Telegram uses a Bot Token (not OAuth). The user creates a bot via @BotFather, adds it to a channel as admin, stores the bot token in OneVoice. The agent handles:
- `telegram__send_channel_post`: `sendMessage` (text) or `sendPhoto`/`sendDocument` (media)
- `telegram__send_notification`: `sendMessage` to the owner's personal chat

**Library:** `github.com/go-telegram-bot-api/telegram-bot-api/v5`
**Rate limits:** Telegram returns `429 Too Many Requests` with `retry_after` seconds — handle with backoff.

### Step 1: Create go.mod

```bash
mkdir -p services/agent-telegram/internal/agent services/agent-telegram/cmd
cat > services/agent-telegram/go.mod << 'EOF'
module github.com/f1xgun/onevoice/services/agent-telegram

go 1.24.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
    github.com/f1xgun/onevoice/pkg v0.0.0
    github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1
    github.com/nats-io/nats.go v1.41.1
    github.com/stretchr/testify v1.11.1
)
EOF
cd services/agent-telegram && go mod tidy
```

### Step 2: Write failing handler tests

Create `services/agent-telegram/internal/agent/handler_test.go`:

```go
package agent_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSender simulates Telegram bot API without a real connection.
type fakeSender struct {
	sentMessage string
	sentChatID  int64
}

func (f *fakeSender) SendMessage(chatID int64, text string) error {
	f.sentMessage = text
	f.sentChatID = chatID
	return nil
}

func TestHandler_SendChannelPost(t *testing.T) {
	sender := &fakeSender{}
	h := agent.NewHandler(sender)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "telegram__send_channel_post",
		Args: map[string]interface{}{
			"text":       "Hello, channel!",
			"channel_id": "-1001234567890",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello, channel!", sender.sentMessage)
}

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	h := agent.NewHandler(&fakeSender{})

	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "telegram__unknown_tool",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
```

Run: `cd /Users/f1xgun/onevoice/services/agent-telegram && go test ./internal/agent/...`
Expected: FAIL — `agent.NewHandler` not defined

### Step 3: Implement `handler.go`

Create `services/agent-telegram/internal/agent/handler.go`:

```go
package agent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// Sender abstracts Telegram message sending for testability.
type Sender interface {
	SendMessage(chatID int64, text string) error
}

// Handler implements a2a.Handler for the Telegram agent.
type Handler struct {
	sender Sender
}

// NewHandler creates a Handler with the given Telegram sender.
func NewHandler(sender Sender) *Handler {
	return &Handler{sender: sender}
}

// Handle routes the ToolRequest to the appropriate Telegram operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "telegram__send_channel_post":
		return h.sendChannelPost(req)
	case "telegram__send_notification":
		return h.sendNotification(req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) sendChannelPost(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	chatID, err := strconv.ParseInt(channelIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, err)
	}

	if err := h.sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send message: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

func (h *Handler) sendNotification(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("telegram: invalid chat_id %q: %w", chatIDStr, err)
	}

	if err := h.sender.SendMessage(chatID, text); err != nil {
		return nil, fmt.Errorf("telegram: send notification: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}
```

### Step 4: Implement Telegram Bot sender

Create `services/agent-telegram/internal/telegram/bot.go`:

```go
package telegram

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot wraps the Telegram Bot API client.
type Bot struct {
	api *tgbotapi.BotAPI
}

// New creates a Bot with the given token.
func New(token string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	return &Bot{api: api}, nil
}

// SendMessage sends a text message to the given chat ID.
func (b *Bot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}
```

### Step 5: Implement cmd/main.go

Create `services/agent-telegram/cmd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	agentpkg "github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

func main() {
	log := logger.New("agent-telegram")
	slog.SetDefault(log)

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		log.Error("failed to connect to NATS", "url", natsURL, "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	bot, err := telegram.New(botToken)
	if err != nil {
		log.Error("failed to create Telegram bot", "error", err)
		os.Exit(1)
	}

	handler := agentpkg.NewHandler(bot)
	transport := a2a.NewNATSTransport(nc)
	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.Start(ctx); err != nil {
		log.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	log.Info("telegram agent started, listening on NATS", "subject", a2a.Subject(a2a.AgentTelegram))
	<-ctx.Done()
	log.Info("telegram agent shutting down")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
```

### Step 6: Verify GREEN

```bash
cd /Users/f1xgun/onevoice/services/agent-telegram && go test -race ./... && go build ./cmd/...
```
Expected: PASS + builds

### Step 7: Commit

```bash
cd /Users/f1xgun/onevoice && \
  git add services/agent-telegram/ && \
  git commit -m "feat(agent-telegram): Telegram agent with send_channel_post and send_notification"
```

---

## Task 3: VK Agent service scaffold + handler

**Files:**
- Create: `services/agent-vk/go.mod`
- Create: `services/agent-vk/internal/agent/handler.go`
- Create: `services/agent-vk/internal/agent/handler_test.go`
- Create: `services/agent-vk/internal/vk/client.go`
- Create: `services/agent-vk/cmd/main.go`

**Context:** VK uses OAuth 2.0. The access token is provided via `VK_ACCESS_TOKEN` environment variable (in production, fetched from encrypted DB via internal API). VK token with `offline` scope doesn't expire. Tools:
- `vk__publish_post`: `wall.post` API call (text + optional attachment)
- `vk__update_group_info`: `groups.edit` API call
- `vk__get_comments`: `wall.getComments` API call

**Library:** `github.com/SevereCloud/vksdk/v3`

### Step 1: Create go.mod

```bash
mkdir -p services/agent-vk/internal/agent services/agent-vk/internal/vk services/agent-vk/cmd
cat > services/agent-vk/go.mod << 'EOF'
module github.com/f1xgun/onevoice/services/agent-vk

go 1.24.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
    github.com/f1xgun/onevoice/pkg v0.0.0
    github.com/SevereCloud/vksdk/v3 v3.1.0
    github.com/nats-io/nats.go v1.41.1
    github.com/stretchr/testify v1.11.1
)
EOF
cd services/agent-vk && go mod tidy
```

### Step 2: Write failing handler tests

Create `services/agent-vk/internal/agent/handler_test.go`:

```go
package agent_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVKClient struct {
	lastPostText    string
	lastGroupID     string
}

func (f *fakeVKClient) PublishPost(groupID, text string) (int64, error) {
	f.lastPostText = text
	f.lastGroupID = groupID
	return 123, nil
}

func (f *fakeVKClient) UpdateGroupInfo(groupID, description string) error {
	f.lastGroupID = groupID
	return nil
}

func (f *fakeVKClient) GetComments(groupID string, count int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": "1", "text": "nice!"}}, nil
}

func TestHandler_PublishPost(t *testing.T) {
	client := &fakeVKClient{}
	h := agent.NewHandler(client)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "vk__publish_post",
		Args: map[string]interface{}{
			"text":     "Hello VK!",
			"group_id": "-123456",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello VK!", client.lastPostText)
	assert.Equal(t, float64(123), resp.Result["post_id"])
}

func TestHandler_UnknownTool_ReturnsError(t *testing.T) {
	h := agent.NewHandler(&fakeVKClient{})
	_, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "vk__nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
```

Run: `cd /Users/f1xgun/onevoice/services/agent-vk && go test ./internal/agent/...`
Expected: FAIL

### Step 3: Implement handler.go

Create `services/agent-vk/internal/agent/handler.go`:

```go
package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// VKClient abstracts VK API operations for testability.
type VKClient interface {
	PublishPost(groupID, text string) (int64, error)
	UpdateGroupInfo(groupID, description string) error
	GetComments(groupID string, count int) ([]map[string]interface{}, error)
}

// Handler implements a2a.Handler for the VK agent.
type Handler struct {
	client VKClient
}

// NewHandler creates a Handler with the given VK client.
func NewHandler(client VKClient) *Handler {
	return &Handler{client: client}
}

// Handle routes the ToolRequest to the appropriate VK API operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "vk__publish_post":
		return h.publishPost(req)
	case "vk__update_group_info":
		return h.updateGroupInfo(req)
	case "vk__get_comments":
		return h.getComments(req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) publishPost(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	groupID, _ := req.Args["group_id"].(string)

	postID, err := h.client.PublishPost(groupID, text)
	if err != nil {
		return nil, fmt.Errorf("vk: publish post: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"post_id": float64(postID)},
	}, nil
}

func (h *Handler) updateGroupInfo(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	groupID, _ := req.Args["group_id"].(string)
	description, _ := req.Args["description"].(string)

	if err := h.client.UpdateGroupInfo(groupID, description); err != nil {
		return nil, fmt.Errorf("vk: update group info: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated"},
	}, nil
}

func (h *Handler) getComments(req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	groupID, _ := req.Args["group_id"].(string)
	countF, _ := req.Args["count"].(float64)
	count := int(countF)
	if count == 0 {
		count = 20
	}

	comments, err := h.client.GetComments(groupID, count)
	if err != nil {
		return nil, fmt.Errorf("vk: get comments: %w", err)
	}

	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"comments": comments, "count": len(comments)},
	}, nil
}
```

### Step 4: Implement VK client wrapper

Create `services/agent-vk/internal/vk/client.go`:

```go
package vk

import (
	"fmt"

	vkapi "github.com/SevereCloud/vksdk/v3/api"
)

// Client wraps the VK SDK for wall and group operations.
type Client struct {
	vk *vkapi.VK
}

// New creates a VK Client with the given access token.
func New(accessToken string) *Client {
	return &Client{vk: vkapi.NewVK(accessToken)}
}

// PublishPost publishes a post to the VK community wall.
// groupID must be negative (e.g., "-123456").
func (c *Client) PublishPost(groupID, text string) (int64, error) {
	resp, err := c.vk.WallPost(vkapi.Params{
		"owner_id": groupID,
		"message":  text,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.post: %w", err)
	}
	return int64(resp.PostID), nil
}

// UpdateGroupInfo updates the VK community description.
func (c *Client) UpdateGroupInfo(groupID, description string) error {
	_, err := c.vk.GroupsEdit(vkapi.Params{
		"group_id":    groupID,
		"description": description,
	})
	return err
}

// GetComments retrieves comments from the VK community wall.
func (c *Client) GetComments(groupID string, count int) ([]map[string]interface{}, error) {
	resp, err := c.vk.WallGetComments(vkapi.Params{
		"owner_id": groupID,
		"count":    count,
		"extended": 0,
	})
	if err != nil {
		return nil, fmt.Errorf("vk wall.getComments: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Items))
	for _, item := range resp.Items {
		result = append(result, map[string]interface{}{
			"id":   item.ID,
			"text": item.Text,
			"date": item.Date,
		})
	}
	return result, nil
}
```

### Step 5: Implement cmd/main.go

Create `services/agent-vk/cmd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	agentpkg "github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/vk"
)

func main() {
	log := logger.New("agent-vk")
	slog.SetDefault(log)

	accessToken := os.Getenv("VK_ACCESS_TOKEN")
	if accessToken == "" {
		log.Error("VK_ACCESS_TOKEN is required")
		os.Exit(1)
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		log.Error("failed to connect to NATS", "url", natsURL, "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	client := vk.New(accessToken)
	handler := agentpkg.NewHandler(client)
	transport := a2a.NewNATSTransport(nc)
	agent := a2a.NewAgent(a2a.AgentVK, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.Start(ctx); err != nil {
		log.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	log.Info("VK agent started, listening on NATS", "subject", a2a.Subject(a2a.AgentVK))
	<-ctx.Done()
	log.Info("VK agent shutting down")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
```

### Step 6: Verify GREEN

```bash
cd /Users/f1xgun/onevoice/services/agent-vk && go test -race ./... && go build ./cmd/...
```
Expected: PASS + builds

### Step 7: Commit

```bash
cd /Users/f1xgun/onevoice && \
  git add services/agent-vk/ && \
  git commit -m "feat(agent-vk): VK agent with publish_post, update_group_info, get_comments"
```

---

## Task 4: Yandex.Business Agent (RPA) — Playwright

**Files:**
- Create: `services/agent-yandex-business/go.mod`
- Create: `services/agent-yandex-business/internal/agent/handler.go`
- Create: `services/agent-yandex-business/internal/agent/handler_test.go`
- Create: `services/agent-yandex-business/internal/yandex/browser.go`
- Create: `services/agent-yandex-business/internal/yandex/update_hours.go`
- Create: `services/agent-yandex-business/internal/yandex/update_info.go`
- Create: `services/agent-yandex-business/internal/yandex/get_reviews.go`
- Create: `services/agent-yandex-business/internal/yandex/reply_review.go`
- Create: `services/agent-yandex-business/cmd/main.go`

**Context:** Yandex.Business has NO public API (as of Feb 2026). The agent uses Playwright (`playwright-go`) to automate the `https://business.yandex.ru` web interface. Auth is done via Yandex ID cookies (stored encrypted in DB; provided via env var `YANDEX_COOKIES_JSON` for MVP).

**Key RPA requirements:**
- Resilient CSS selectors with fallback strategy (primary → secondary → XPath)
- Human-like delays: `time.Sleep(rand.Intn(3000) + 1000) * time.Millisecond`
- Screenshot on error for diagnostics (save to `/tmp/yandex_error_*.png`)
- Retry with exponential backoff (3 attempts, 2^n seconds)
- Canary check before operations (verify expected DOM elements exist)
- Stealth mode: disable `navigator.webdriver`

**Library:** `github.com/playwright-community/playwright-go`

### Step 1: Create go.mod

```bash
mkdir -p services/agent-yandex-business/internal/{agent,yandex} services/agent-yandex-business/cmd
cat > services/agent-yandex-business/go.mod << 'EOF'
module github.com/f1xgun/onevoice/services/agent-yandex-business

go 1.24.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
    github.com/f1xgun/onevoice/pkg v0.0.0
    github.com/playwright-community/playwright-go v0.4501.1
    github.com/nats-io/nats.go v1.41.1
    github.com/stretchr/testify v1.11.1
)
EOF
cd services/agent-yandex-business && go mod tidy
```

### Step 2: Write failing handler tests (using stub browser)

Create `services/agent-yandex-business/internal/agent/handler_test.go`:

```go
package agent_test

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBrowser implements YandexBrowser for tests (no real browser).
type stubBrowser struct {
	updatedHours  string
	updatedInfo   map[string]string
	reviews       []map[string]interface{}
	repliedID     string
	repliedText   string
}

func (s *stubBrowser) UpdateHours(ctx context.Context, hoursJSON string) error {
	s.updatedHours = hoursJSON
	return nil
}

func (s *stubBrowser) UpdateInfo(ctx context.Context, info map[string]string) error {
	s.updatedInfo = info
	return nil
}

func (s *stubBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	s.reviews = []map[string]interface{}{{"id": "r1", "text": "Отличное место!", "rating": 5}}
	return s.reviews, nil
}

func (s *stubBrowser) ReplyReview(ctx context.Context, reviewID, text string) error {
	s.repliedID = reviewID
	s.repliedText = text
	return nil
}

func TestHandler_UpdateHours(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t1",
		Tool:   "yandex_business__update_hours",
		Args:   map[string]interface{}{"hours": `{"mon":"09:00-21:00"}`},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, `{"mon":"09:00-21:00"}`, browser.updatedHours)
}

func TestHandler_GetReviews(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t2",
		Tool:   "yandex_business__get_reviews",
		Args:   map[string]interface{}{"limit": float64(10)},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	reviews, ok := resp.Result["reviews"].([]map[string]interface{})
	require.True(t, ok)
	assert.Len(t, reviews, 1)
}

func TestHandler_ReplyReview(t *testing.T) {
	browser := &stubBrowser{}
	h := agent.NewHandler(browser)

	resp, err := h.Handle(context.Background(), a2a.ToolRequest{
		TaskID: "t3",
		Tool:   "yandex_business__reply_review",
		Args: map[string]interface{}{
			"review_id": "r1",
			"text":      "Спасибо за отзыв!",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "r1", browser.repliedID)
	assert.Equal(t, "Спасибо за отзыв!", browser.repliedText)
}

func TestHandler_UnknownTool(t *testing.T) {
	h := agent.NewHandler(&stubBrowser{})
	_, err := h.Handle(context.Background(), a2a.ToolRequest{Tool: "yandex_business__unknown"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}
```

Run: `cd /Users/f1xgun/onevoice/services/agent-yandex-business && go test ./internal/agent/...`
Expected: FAIL

### Step 3: Implement handler.go

Create `services/agent-yandex-business/internal/agent/handler.go`:

```go
package agent

import (
	"context"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// YandexBrowser abstracts Playwright browser operations for testability.
type YandexBrowser interface {
	UpdateHours(ctx context.Context, hoursJSON string) error
	UpdateInfo(ctx context.Context, info map[string]string) error
	GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error)
	ReplyReview(ctx context.Context, reviewID, text string) error
}

// Handler implements a2a.Handler for the Yandex.Business RPA agent.
type Handler struct {
	browser YandexBrowser
}

// NewHandler creates a Handler with the given browser automation implementation.
func NewHandler(browser YandexBrowser) *Handler {
	return &Handler{browser: browser}
}

// Handle routes ToolRequests to the appropriate Yandex.Business operation.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	switch req.Tool {
	case "yandex_business__update_hours":
		return h.updateHours(ctx, req)
	case "yandex_business__update_info":
		return h.updateInfo(ctx, req)
	case "yandex_business__get_reviews":
		return h.getReviews(ctx, req)
	case "yandex_business__reply_review":
		return h.replyReview(ctx, req)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Tool)
	}
}

func (h *Handler) updateHours(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	hours, _ := req.Args["hours"].(string)
	if err := h.browser.UpdateHours(ctx, hours); err != nil {
		return nil, fmt.Errorf("yandex: update hours: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) updateInfo(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	info := make(map[string]string)
	for _, key := range []string{"phone", "website", "description"} {
		if v, ok := req.Args[key].(string); ok {
			info[key] = v
		}
	}
	if err := h.browser.UpdateInfo(ctx, info); err != nil {
		return nil, fmt.Errorf("yandex: update info: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "updated", "note": "changes pending Yandex moderation"},
	}, nil
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limitF, _ := req.Args["limit"].(float64)
	limit := int(limitF)
	if limit == 0 {
		limit = 20
	}

	reviews, err := h.browser.GetReviews(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("yandex: get reviews: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"reviews": reviews, "count": len(reviews)},
	}, nil
}

func (h *Handler) replyReview(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	reviewID, _ := req.Args["review_id"].(string)
	text, _ := req.Args["text"].(string)

	if err := h.browser.ReplyReview(ctx, reviewID, text); err != nil {
		return nil, fmt.Errorf("yandex: reply review: %w", err)
	}
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "replied"},
	}, nil
}
```

### Step 4: Implement browser.go (Playwright wrapper)

Create `services/agent-yandex-business/internal/yandex/browser.go`:

```go
package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/playwright-community/playwright-go"
)

// Browser implements YandexBrowser using Playwright for RPA automation.
type Browser struct {
	cookiesJSON string // Yandex ID session cookies as JSON array
}

// NewBrowser creates a Browser with the given Yandex session cookies.
func NewBrowser(cookiesJSON string) *Browser {
	return &Browser{cookiesJSON: cookiesJSON}
}

const businessURL = "https://business.yandex.ru"

// withPage creates a Playwright browser page, sets cookies, and calls fn.
// Screenshots are taken on error for diagnostics.
func (b *Browser) withPage(ctx context.Context, fn func(page playwright.Page) error) error {
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright: run: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
			"--no-sandbox",
		},
	})
	if err != nil {
		return fmt.Errorf("playwright: launch: %w", err)
	}
	defer browser.Close()

	bCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	})
	if err != nil {
		return fmt.Errorf("playwright: new context: %w", err)
	}
	defer bCtx.Close()

	// Inject Yandex ID cookies
	if err := b.setCookies(bCtx); err != nil {
		return fmt.Errorf("playwright: set cookies: %w", err)
	}

	page, err := bCtx.NewPage()
	if err != nil {
		return fmt.Errorf("playwright: new page: %w", err)
	}

	if err := fn(page); err != nil {
		// Screenshot on error for diagnostics
		filename := fmt.Sprintf("/tmp/yandex_error_%d.png", time.Now().UnixMilli())
		page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filename)})
		return err
	}
	return nil
}

func (b *Browser) setCookies(bCtx playwright.BrowserContext) error {
	var cookies []map[string]interface{}
	if err := json.Unmarshal([]byte(b.cookiesJSON), &cookies); err != nil {
		return fmt.Errorf("parse cookies JSON: %w", err)
	}

	pwCookies := make([]playwright.OptionalCookie, 0, len(cookies))
	for _, c := range cookies {
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		domain, _ := c["domain"].(string)
		path, _ := c["path"].(string)
		cookie := playwright.OptionalCookie{
			Name:   name,
			Value:  value,
			Domain: playwright.String(domain),
			Path:   playwright.String(path),
		}
		pwCookies = append(pwCookies, cookie)
	}
	return bCtx.AddCookies(pwCookies)
}

// humanDelay waits a random 1–4 seconds to mimic human behavior.
func humanDelay() {
	time.Sleep(time.Duration(rand.Intn(3000)+1000) * time.Millisecond)
}

// withRetry retries fn up to maxAttempts times with exponential backoff.
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for i := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if i < maxAttempts-1 {
			backoff := time.Duration(1<<uint(i)) * time.Second
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
```

### Step 5: Implement update_hours.go (Playwright RPA)

Create `services/agent-yandex-business/internal/yandex/update_hours.go`:

```go
package yandex

import (
	"context"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
)

// UpdateHours updates business operating hours in Yandex.Business.
// Uses RPA automation since no public API exists.
func (b *Browser) UpdateHours(ctx context.Context, hoursJSON string) error {
	return withRetry(ctx, 3, func() error {
		return b.withPage(ctx, func(page playwright.Page) error {
			// Navigate to business hours settings
			if _, err := page.Goto(businessURL+"/settings/hours", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to hours settings: %w", err)
			}

			humanDelay()

			// Canary check: verify we're on the right page
			if err := page.WaitForSelector("[data-testid='hours-form'], .hours-editor", playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(10000),
			}); err != nil {
				return fmt.Errorf("canary check failed: hours form not found: %w", err)
			}

			// TODO: implement actual hours form interaction based on hoursJSON
			// This is a stub — real implementation requires inspecting Yandex.Business DOM
			_ = hoursJSON
			_ = time.Now()

			return fmt.Errorf("yandex.business hours RPA: selector mapping not yet implemented — requires DOM inspection")
		})
	})
}
```

**Note for implementer:** The actual Playwright selectors for Yandex.Business forms must be determined by inspecting `https://business.yandex.ru/settings/hours` with a real Yandex ID account. The stub above documents the pattern; fill in real selectors once inspected. The approach:
1. Navigate to the page
2. Canary check DOM
3. For each day, find the toggle and time inputs
4. Fill values and save
5. Verify confirmation toast appears

### Step 6: Implement get_reviews.go, reply_review.go, update_info.go (same pattern)

Follow the same `withRetry` + `withPage` + `humanDelay` + canary check pattern for each operation. The key API surfaces are:
- Reviews: scrape the reviews list from `https://business.yandex.ru/reviews`
- Reply: find the reply textarea for the given review ID and submit
- Info: navigate to contacts settings and update fields

### Step 7: Implement cmd/main.go

Create `services/agent-yandex-business/cmd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
)

func main() {
	log := logger.New("agent-yandex-business")
	slog.SetDefault(log)

	cookiesJSON := os.Getenv("YANDEX_COOKIES_JSON")
	if cookiesJSON == "" {
		log.Error("YANDEX_COOKIES_JSON is required (Yandex ID session cookies as JSON array)")
		os.Exit(1)
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		log.Error("failed to connect to NATS", "url", natsURL, "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	browser := yandex.NewBrowser(cookiesJSON)
	handler := agentpkg.NewHandler(browser)
	transport := a2a.NewNATSTransport(nc)
	agent := a2a.NewAgent(a2a.AgentYandexBusiness, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.Start(ctx); err != nil {
		log.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	log.Info("Yandex.Business RPA agent started", "subject", a2a.Subject(a2a.AgentYandexBusiness))
	<-ctx.Done()
	log.Info("Yandex.Business agent shutting down")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
```

### Step 8: Verify GREEN (handler tests only — no real browser in CI)

```bash
cd /Users/f1xgun/onevoice/services/agent-yandex-business && \
  go test -race ./internal/agent/... && \
  go build ./cmd/...
```
Expected: handler tests PASS, build succeeds

### Step 9: Commit

```bash
cd /Users/f1xgun/onevoice && \
  git add services/agent-yandex-business/ && \
  git commit -m "feat(agent-yandex-business): Yandex.Business RPA agent with Playwright automation"
```

---

## Task 5: Update docker-compose.yml + update go.work

**Files:**
- Modify: `go.work` (add new agent modules)
- Modify: `deployments/docker/docker-compose.yml` (add agent services)

### Step 1: Update go.work

```
go work use ./services/agent-telegram ./services/agent-vk ./services/agent-yandex-business
```

### Step 2: Add agent services to docker-compose.yml

Add to `deployments/docker/docker-compose.yml`:

```yaml
  agent-telegram:
    build:
      context: ../..
      dockerfile: deployments/docker/Dockerfile.agent-telegram
    environment:
      - TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
      - NATS_URL=nats://nats:4222
    depends_on:
      - nats
    restart: unless-stopped

  agent-vk:
    build:
      context: ../..
      dockerfile: deployments/docker/Dockerfile.agent-vk
    environment:
      - VK_ACCESS_TOKEN=${VK_ACCESS_TOKEN}
      - NATS_URL=nats://nats:4222
    depends_on:
      - nats
    restart: unless-stopped

  agent-yandex-business:
    build:
      context: ../..
      dockerfile: deployments/docker/Dockerfile.agent-yandex-business
    environment:
      - YANDEX_COOKIES_JSON=${YANDEX_COOKIES_JSON}
      - NATS_URL=nats://nats:4222
    depends_on:
      - nats
    restart: unless-stopped
```

### Step 3: Create Dockerfiles

Create `deployments/docker/Dockerfile.agent-telegram`:
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.work go.work.sum ./
COPY pkg/ pkg/
COPY services/agent-telegram/ services/agent-telegram/
RUN cd services/agent-telegram && go build -o /agent ./cmd/...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /agent /agent
ENTRYPOINT ["/agent"]
```

(Same pattern for agent-vk and agent-yandex-business; agent-yandex-business needs Playwright chromium installed.)

`Dockerfile.agent-yandex-business` must also install Playwright browser:
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.work go.work.sum ./
COPY pkg/ pkg/
COPY services/agent-yandex-business/ services/agent-yandex-business/
RUN cd services/agent-yandex-business && go build -o /agent ./cmd/...

FROM mcr.microsoft.com/playwright:v1.50.0-jammy
COPY --from=builder /agent /agent
ENTRYPOINT ["/agent"]
```

### Step 4: Verify workspace compiles

```bash
cd /Users/f1xgun/onevoice && go build ./services/agent-telegram/... ./services/agent-vk/... ./services/agent-yandex-business/...
```

### Step 5: Commit

```bash
cd /Users/f1xgun/onevoice && \
  git add go.work deployments/docker/ && \
  git commit -m "feat: add agent services to docker-compose and go.work"
```

---

## Verification Checklist

After all 5 tasks complete, run from repo root:

```bash
# Run all handler tests (no real NATS/Telegram/VK/Playwright needed)
cd pkg && go test -race ./a2a/...
cd services/agent-telegram && go test -race ./internal/agent/...
cd services/agent-vk && go test -race ./internal/agent/...
cd services/agent-yandex-business && go test -race ./internal/agent/...

# Build all agents
go build ./services/agent-telegram/cmd/...
go build ./services/agent-vk/cmd/...
go build ./services/agent-yandex-business/cmd/...
```

Expected: all PASS, all build

### Integration smoke test (requires real credentials):

1. Start docker-compose: `make up`
2. Set env vars: `TELEGRAM_BOT_TOKEN`, `VK_ACCESS_TOKEN`, `YANDEX_COOKIES_JSON`
3. Call orchestrator: `POST /chat/test-conv {"model":"gpt-4o-mini","message":"Опубликуй пост в Telegram: Тест системы"}`
4. Verify: SSE stream shows `telegram__send_channel_post` tool call + success response
