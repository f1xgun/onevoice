# Go Anti-Patterns — OneVoice Backend

Mistakes to avoid. Most are enforced by linters; the ones that aren't still get caught in review. For the rules behind these, see [go-style.md](go-style.md). For the correct shape, see [go-patterns.md](go-patterns.md).

---

## Creating helpers when shared utils exist

```go
// BAD: Writing a new encrypt function
func encryptToken(token string) (string, error) { /* ... */ }

// GOOD: Use the shared encryptor in pkg/crypto
encrypted, err := s.encryptor.Encrypt([]byte(token))
```

Check `pkg/` first. Shared packages:

- `pkg/crypto` — AES-256-GCM
- `pkg/domain` — models, sentinel errors, repository interfaces
- `pkg/llm` — provider-agnostic LLM dispatch
- `pkg/a2a` — agent-to-agent protocol + base `Agent`
- `pkg/logger` — structured slog
- `pkg/health` — liveness/readiness probes
- `pkg/metrics` — Prometheus counters/histograms
- `pkg/tokenclient` — fetch decrypted integration tokens (used by agents)

## Ignoring errors

```go
// BAD: Silently ignored (caught by errcheck)
result, _ := doSomething()

// BAD: also caught — assigning to blank then branching on nil
result, _ := s.repo.Get(ctx, id)
if result == nil { return ErrNotFound }
```

```go
// GOOD: Handle, or document why ignoring is intentional
result, err := doSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)
}
```

Only place `_ :=` is acceptable is after an explicit comment — e.g. `_, _ = fmt.Fprintln(w, "ok") // best-effort SSE write`.

## Unwrapped errors

```go
// BAD: Loses the original chain; callers can't errors.Is() the root cause
return errors.New("failed to save user")

// GOOD: Wrap with context
return fmt.Errorf("save user: %w", err)
```

## `os.Setenv` in tests

```go
// BAD: Leaks env into sibling tests, breaks parallelism
func TestConfig(t *testing.T) {
    os.Setenv("PORT", "9090")
}

// GOOD: Auto-restores on cleanup
func TestConfig(t *testing.T) {
    t.Setenv("PORT", "9090")
}
```

## `context.Background()` in business logic

```go
// BAD: Ignores caller's cancellation / deadline
func (s *Service) Process() error {
    ctx := context.Background()
    return s.repo.Save(ctx, data)
}

// GOOD: Accept and propagate
func (s *Service) Process(ctx context.Context) error {
    return s.repo.Save(ctx, data)
}
```

`context.Background()` is only acceptable in:

- `main()`
- Tests
- Fire-and-forget goroutines whose failure is logged but doesn't affect the caller (e.g. async billing)

## Hand-rolled validation

```go
// BAD: Manual string checks, drift between endpoints
if req.Email == "" {
    return errors.New("email required")
}
if !strings.Contains(req.Email, "@") {
    return errors.New("invalid email")
}
```

```go
// GOOD: struct tags + validator/v10
type CreateUserRequest struct {
    Email string `json:"email" validate:"required,email"`
    Name  string `json:"name"  validate:"required,min=1,max=100"`
}
if err := validate.Struct(req); err != nil {
    return err  // handler maps to 400
}
```

## Skipping layers

```go
// BAD: handler bypasses service and talks to repository
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.userRepo.GetByID(r.Context(), id)
    // ... encode, write
}

// GOOD: handler → service → repository
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.userService.GetByID(r.Context(), id)
    // ... encode, write
}
```

The service is where authorization checks, caching, billing, and cross-cutting concerns live.

## SQL string-concatenation

```go
// BAD: SQL injection
query := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email)
rows, _ := db.Query(query)  // also: ignored error

// GOOD: squirrel + pgx
query, args, err := squirrel.Select("id", "email").From("users").
    Where(squirrel.Eq{"email": email}).PlaceholderFormat(squirrel.Dollar).ToSql()
```

## Stringly-typed IDs

```go
// BAD: easy to mix up userID and businessID
func (s *Service) Assign(ctx context.Context, userID, businessID string) error

// GOOD: uuid.UUID at the boundary
func (s *Service) Assign(ctx context.Context, userID, businessID uuid.UUID) error
```

The handler parses the string once; downstream code gets type safety.

## Logging secrets

```go
// BAD — will leak
logger.InfoContext(ctx, "saved token", "access_token", token)

// GOOD — log a redacted identifier, never the secret itself
logger.InfoContext(ctx, "saved token",
    "integration_id", integrationID,
    "platform", platform,
)
```

Tokens, passwords, refresh tokens, full JWTs, cookies — never in logs. Enforced by code review; `gosec` catches some obvious cases.

## Ignoring `ctx.Done()` in long loops

```go
// BAD: ignores cancellation; tests that use ctx with deadline will hang
for page := 1; page <= 100; page++ {
    posts, err := api.ListPosts(page)
    // ...
}

// GOOD: check cancellation each iteration
for page := 1; page <= 100; page++ {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    posts, err := api.ListPosts(ctx, page)
    // ...
}
```

Especially important in RPA loops (Playwright) where each iteration can take seconds.

## Starting long-running goroutines without a way to stop them

```go
// BAD: no way to cancel this goroutine
go func() {
    for {
        time.Sleep(5 * time.Second)
        doWork()
    }
}()

// GOOD: ctx-bound, logs on exit
go func() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := doWork(ctx); err != nil {
                logger.ErrorContext(ctx, "work failed", "err", err)
            }
        }
    }
}()
```

## Returning nil + error together

```go
// BAD: callers must check both; easy to miss one
func (r *Repo) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
    // ...
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, domain.ErrUserNotFound
    }
    return nil, err  // forgotten earlier: returning a partial user
}

// GOOD: on error, always return nil pointer; on success, always non-nil
// Callers check err first, then use the pointer.
```

## Blocking on unbuffered channels without a select

```go
// BAD: deadlocks if the receiver is gone
eventCh <- event

// GOOD: cancellable
select {
case eventCh <- event:
case <-ctx.Done():
    return ctx.Err()
}
```

Especially in SSE paths where the client can disconnect at any time.
