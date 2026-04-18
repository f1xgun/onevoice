# Go Patterns — OneVoice Backend

Good/right-way examples used consistently across `pkg/`, `services/api/`, `services/orchestrator/`, and the platform agents. For the *rules* behind these patterns, see [go-style.md](go-style.md). For mistakes to avoid, see [go-antipatterns.md](go-antipatterns.md).

---

## Error Handling

Wrap every error with a short context prefix — never return bare `err`. Convert infrastructure-specific errors to domain errors at the repository boundary.

```go
user, err := s.userRepo.GetByID(ctx, id)
if err != nil {
    return fmt.Errorf("get user: %w", err)
}
```

Convert `pgx.ErrNoRows` and MongoDB `mongo.ErrNoDocuments` to domain errors right at the repo:

```go
if errors.Is(err, pgx.ErrNoRows) {
    return nil, domain.ErrUserNotFound
}
```

Sentinel errors live in `pkg/domain/errors.go`:

```go
var (
    ErrUserNotFound   = errors.New("user not found")
    ErrUserExists     = errors.New("user already exists")
    ErrInvalidToken   = errors.New("invalid token")
    ErrRateLimitExceeded = errors.New("rate limit exceeded")
)
```

Callers check with `errors.Is` / `errors.As`, never string matching.

## Repository Pattern (pgx + squirrel)

```go
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
    query, args, err := squirrel.
        Select("id", "email", "password_hash", "role", "created_at", "updated_at").
        From("users").
        Where(squirrel.Eq{"email": email}).
        PlaceholderFormat(squirrel.Dollar).
        ToSql()
    if err != nil {
        return nil, fmt.Errorf("build query: %w", err)
    }

    var user domain.User
    err = r.pool.QueryRow(ctx, query, args...).Scan(
        &user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt,
    )
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, domain.ErrUserNotFound
        }
        return nil, fmt.Errorf("query user: %w", err)
    }
    return &user, nil
}
```

## Transactions

Always `defer tx.Rollback()` — harmless if `Commit()` already succeeded, but guarantees cleanup on every error path.

```go
tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
if err != nil {
    return fmt.Errorf("begin tx: %w", err)
}
defer func() { _ = tx.Rollback(ctx) }()

if err := r.insertBusiness(ctx, tx, biz); err != nil {
    return fmt.Errorf("insert business: %w", err)
}
if err := r.insertSchedule(ctx, tx, biz.ID, schedule); err != nil {
    return fmt.Errorf("insert schedule: %w", err)
}
return tx.Commit(ctx)
```

## Testing

```go
func TestService_Create(t *testing.T) {
    tests := []struct {
        name      string
        mockErr   error
        wantErr   error
    }{
        {name: "success", mockErr: nil, wantErr: nil},
        {name: "duplicate", mockErr: domain.ErrUserExists, wantErr: domain.ErrUserExists},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            repo := &mockRepo{createFunc: func(context.Context, *domain.User) error { return tt.mockErr }}
            svc := NewService(repo)
            err := svc.Create(context.Background(), input)
            if tt.wantErr != nil {
                assert.ErrorIs(t, err, tt.wantErr)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

Environment variables in tests use `t.Setenv`, not `os.Setenv`:

```go
func TestConfigLoad(t *testing.T) {
    t.Setenv("LLM_MODEL", "gpt-4o-mini")
    cfg, err := config.Load()
    require.NoError(t, err)
    assert.Equal(t, "gpt-4o-mini", cfg.Model)
}
```

## NATS Tool Dispatch

Orchestrator → agent via NATS request/reply. Tool name is plain `string`; platform is extracted from `{platform}__{action}`.

```go
tools := registry.Available(activeIntegrations)  // e.g., "telegram__send_channel_post"
```

`NATSExecutor.Execute` sends `a2a.ToolRequest{Tool, BusinessID, Args}` to `tasks.{platform}` and waits for `a2a.ToolResponse`.

## Integration Token Lookup with Fallback

`GetDecryptedToken(ctx, businessID, platform, externalID)` in `services/api/internal/service/integration.go`:

1. If `externalID` is non-empty → try exact match via `GetByBusinessPlatformExternal`.
2. If not found, **or** `externalID` is empty → fall back to first active integration for that platform.
3. Returns `TokenResponse{AccessToken, ExternalID}` — `ExternalID` is the resolved integration's `external_id`.

Result: the LLM hallucinating a business name as a channel ID won't break tool execution — the agent always gets a real, usable token + ID pair.

## Agent Channel-ID Resolution (Telegram)

In the Telegram handlers, after `getSender` returns the resolved `ExternalID`, fall back to it if the LLM-supplied `channel_id` doesn't parse as a number:

```go
chatID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
if parseErr != nil {
    chatID, err = strconv.ParseInt(resolvedID, 10, 64)
    if err != nil {
        return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, parseErr)
    }
}
```

## Tool Call Persistence (chat_proxy)

`services/api/internal/handler/chat_proxy.go` accumulates `tool_call` / `tool_result` SSE events during streaming and persists them on the MongoDB Message:

```go
case "tool_call":
    tc := domain.ToolCall{
        ID:        fmt.Sprintf("tc-%d", len(toolCalls)),
        Name:      ev.ToolName,
        Arguments: ev.ToolArgs,
    }
    toolCalls = append(toolCalls, tc)

case "tool_result":
    toolResults = append(toolResults, domain.ToolResult{
        ToolCallID: tcID,
        Content:    content,
        IsError:    ev.ToolError != "",
    })
```

`GET /conversations/:id/messages` returns both arrays so `useChat.ts` can reconstruct the tool-call panel state on reload.

## SSE Streaming

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("X-Accel-Buffering", "no")

flusher, ok := w.(http.Flusher)
if !ok {
    http.Error(w, "streaming unsupported", http.StatusInternalServerError)
    return
}
for event := range eventCh {
    data, _ := json.Marshal(event)
    fmt.Fprintf(w, "data: %s\n\n", data)
    flusher.Flush()
}
```

Event types: `text`, `tool_call`, `tool_result`, `done`, `error`.

## Async Billing (Fire-and-Forget)

The only sanctioned use of `context.Background()` in business code — a non-critical side effect that must not block the caller:

```go
go r.logBilling(context.Background(), model, usage)
```

The goroutine runs independently; errors are logged but not surfaced to the caller.

## Structured Logging (slog)

Always use `pkg/logger`, never `log.Printf`. Keys are snake_case, values are primitives or small structs:

```go
logger.InfoContext(ctx, "dispatching tool",
    "tool", toolName,
    "business_id", bizID,
    "agent", agentID,
)
```

`InfoContext` picks up correlation IDs automatically if present on the `ctx`.

## Config from Environment

`config.Load()` reads and validates once at startup. Required vars fail fast; optional vars default:

```go
func Load() (*Config, error) {
    model := os.Getenv("LLM_MODEL")
    if model == "" {
        return nil, fmt.Errorf("LLM_MODEL is required")
    }

    port := os.Getenv("PORT")
    if port == "" {
        port = "8090"
    }

    maxIter, err := parseIntEnv("MAX_ITERATIONS", 10)
    if err != nil {
        return nil, fmt.Errorf("MAX_ITERATIONS: %w", err)
    }
    return &Config{Model: model, Port: port, MaxIterations: maxIter}, nil
}
```

## RPA Patterns (Playwright)

Platform: `services/agent-yandex-business/`. Not required for API agents.

### withRetry + withPage + humanDelay

```go
func (c *Client) withRetry(ctx context.Context, fn func() error) error {
    backoff := 500 * time.Millisecond
    for attempt := 0; attempt < 3; attempt++ {
        if attempt > 0 {
            if err := c.canaryCheck(ctx); err != nil {
                return fmt.Errorf("canary failed: %w", err)
            }
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(backoff):
            }
            backoff *= 2
        }
        if err := fn(); err == nil {
            return nil
        }
    }
    return fmt.Errorf("retries exhausted")
}

func (c *Client) withPage(ctx context.Context, fn func(playwright.Page) error) error {
    page, release, err := c.pool.Acquire(ctx)
    if err != nil {
        return fmt.Errorf("acquire page: %w", err)
    }
    defer release()
    return fn(page)
}

func humanDelay() {
    time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)
}
```

### Canary Check

Before running any tool, verify session is still valid (cookies not expired):

```go
func (c *Client) canaryCheck(ctx context.Context) error {
    return c.withPage(ctx, func(p playwright.Page) error {
        if _, err := p.Goto(profileURL); err != nil {
            return err
        }
        if locked, _ := p.IsVisible("text=Войдите"); locked {
            return a2a.NewNonRetryableError(errors.New("session expired"))
        }
        return nil
    })
}
```

Fail fast with `NonRetryableError` so the A2A base agent doesn't waste budget retrying.

### Screenshot-on-Error

On any unexpected error, dump a screenshot to `rpa-screenshots/` for post-mortem:

```go
if err != nil {
    if screenshotErr := page.Screenshot(playwright.PageScreenshotOptions{
        Path: playwright.String(fmt.Sprintf("rpa-screenshots/%s-%d.png", tool, time.Now().Unix())),
    }); screenshotErr != nil {
        logger.ErrorContext(ctx, "screenshot failed", "err", screenshotErr)
    }
    return fmt.Errorf("%s: %w", tool, err)
}
```
