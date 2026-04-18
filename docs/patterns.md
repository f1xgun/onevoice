# Approved Code Patterns

Concrete good/bad examples from across the codebase. For the rule set they implement, see the topic docs: [go-style.md](go-style.md), [frontend-style.md](frontend-style.md), [api-design.md](api-design.md), [security.md](security.md).

## Go Backend

### Error Handling

Always wrap errors with context. Convert DB-specific errors to domain errors.

```go
user, err := s.userRepo.GetByID(ctx, id)
if err != nil {
    return fmt.Errorf("get user: %w", err)
}
```

```go
// Convert pgx errors to domain errors
if errors.Is(err, pgx.ErrNoRows) {
    return nil, domain.ErrUserNotFound
}
```

Define sentinel errors in `pkg/domain/errors.go`:
```go
var ErrUserNotFound = errors.New("user not found")
```

### Repository Pattern (pgx + squirrel)

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
    // ... error handling
}
```

### Testing

- Use `testify/assert` for assertions
- Table-driven tests with `t.Run`
- Mock interfaces via structs with func fields
- Use `t.Setenv` (not `os.Setenv`) — auto-restores on cleanup

```go
func TestService_Create(t *testing.T) {
    t.Run("success", func(t *testing.T) {
        repo := &mockRepo{
            createFunc: func(ctx context.Context, u *domain.User) error {
                return nil
            },
        }
        svc := NewService(repo)
        err := svc.Create(context.Background(), input)
        assert.NoError(t, err)
    })
}
```

### NATS Tool Execution

Tools are dispatched via NATS request/reply. The orchestrator sends A2A protocol messages.

```go
// Tool naming: {platform}__{action}
tools := registry.Available(activeIntegrations)
// e.g., "telegram__send_channel_post" → NATS subject "tasks.telegram"
```

`NATSExecutor.Execute` sends `a2a.ToolRequest{Tool: toolName, BusinessID: ..., Args: ...}` and waits for `a2a.ToolResponse`. The `toolName` field is a plain `string` — no type conversion needed.

### Integration Token Lookup with Fallback

`GetDecryptedToken(ctx, businessID, platform, externalID)` in `services/api/internal/service/integration.go`:
1. If `externalID` non-empty → try exact match via `GetByBusinessPlatformExternal`
2. If not found OR `externalID` empty → fall back to first active integration for that platform
3. Returns `TokenResponse{AccessToken, ExternalID}` — `ExternalID` is the resolved integration's external_id

This means LLM hallucinating a business name as a channel_id won't break tool execution.

### Agent Channel ID Resolution

In Telegram agent handlers (`handler.go`), after `getSender` returns the resolved integration `ExternalID`:

```go
chatID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
if parseErr != nil {
    // LLM passed a non-numeric id — use the integration's own numeric id
    chatID, err = strconv.ParseInt(resolvedID, 10, 64)
    if err != nil {
        return nil, fmt.Errorf("telegram: invalid channel_id %q: %w", channelIDStr, parseErr)
    }
}
```

### Tool Call Persistence in Chat Proxy

`chat_proxy.go` accumulates events during SSE streaming and persists to MongoDB:

```go
case "tool_call":
    tc := domain.ToolCall{ID: fmt.Sprintf("tc-%d", len(toolCalls)), Name: ev.ToolName, Arguments: ev.ToolArgs}
    toolCalls = append(toolCalls, tc)
case "tool_result":
    toolResults = append(toolResults, domain.ToolResult{ToolCallID: tcID, Content: content, IsError: ev.ToolError != ""})
```

Saved on the Message document so `GET /conversations/:id/messages` can return full tool call history.

### SSE Streaming

```go
flusher, _ := w.(http.Flusher)
for event := range eventCh {
    data, _ := json.Marshal(event)
    fmt.Fprintf(w, "data: %s\n\n", data)
    flusher.Flush()
}
```

### Async Billing

Fire-and-forget pattern for non-critical operations:
```go
go r.logBilling(context.Background(), model, usage)
```

### Config from Environment

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
    // ...
}
```

## RPA Agents (Playwright)

### withRetry + withPage + humanDelay

```go
func (y *Client) withRetry(ctx context.Context, fn func() error) error {
    // Retry with exponential backoff, canary check between attempts
}

func (y *Client) withPage(ctx context.Context, fn func(page playwright.Page) error) error {
    // Acquire page from pool, execute fn, release page
}

func humanDelay() {
    // Random 500-1500ms delay to avoid detection
}
```

## Frontend (Next.js / React)

### Zustand Store

```tsx
import { create } from 'zustand';

interface AuthStore {
  user: User | null;
  token: string | null;
  setAuth: (user: User, token: string) => void;
  clearAuth: () => void;
}

export const useAuthStore = create<AuthStore>((set) => ({
  user: null,
  token: null,
  setAuth: (user, token) => set({ user, token }),
  clearAuth: () => set({ user: null, token: null }),
}));
```

### Forms (react-hook-form + zod)

```tsx
const schema = z.object({
  name: z.string().min(1, "Required"),
  email: z.string().email("Invalid email"),
});

type FormData = z.infer<typeof schema>;

export function MyForm() {
  const { register, handleSubmit, formState: { errors } } = useForm<FormData>({
    resolver: zodResolver(schema),
  });
  // ...
}
```

### Component Pattern

- `function` declarations (not `const`)
- TypeScript interfaces for props
- Server components by default, `"use client"` only when needed
- Tailwind classes, never inline styles
