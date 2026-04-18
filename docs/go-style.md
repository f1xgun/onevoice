# Go Style — OneVoice Backend

Detailed Go rules that apply to `pkg/`, `services/api/`, `services/orchestrator/`, and the platform agents (`services/agent-*/`).

For enforced invariants see [golden-principles.md](golden-principles.md). For concrete good/bad snippets see [patterns.md](patterns.md) and [anti-patterns.md](anti-patterns.md).

---

## Project Structure

- **Monorepo with Go workspace** — `go.work` ties `pkg/` and every `services/*` module together
- **Domain-driven** — shared models live in `pkg/domain/`; services import, never redefine
- **Layering per service** — `handler → service → repository`, no layer skipping
- **Internal packages** — use `internal/` to prevent cross-service imports
- **Replace directive** — every service `go.mod` has `replace github.com/f1xgun/onevoice/pkg => ../../pkg`

## Naming Conventions

- Interface names are nouns: `UserRepository`, not `IUserRepository` or `UserRepo`.
- Method names are `Verb + Noun`: `GetByID`, not `Get`.
- Always pass `context.Context` as the first parameter.
- Use `uuid.UUID` for IDs, not `string`.
- Avoid redundant stutter in method names (`CreateUser` on a `UserRepository` — just `Create`).

## Error Handling

- Never ignore errors (`_ := ...` is banned outside tests; `errcheck` enforces this).
- Wrap errors with context: `fmt.Errorf("get user: %w", err)`.
- Define sentinel errors in `pkg/domain/errors.go`: `var ErrUserNotFound = errors.New("user not found")`.
- Use `errors.Is()` / `errors.As()` for error checks, not string matching.
- Convert DB-specific errors (e.g. `pgx.ErrNoRows`) to domain errors at the repository boundary.

## Context & Cancellation

- Always respect `ctx.Done()` in long-running operations; don't `time.Sleep` without a `select`.
- Never create `context.Background()` in business logic — only in `main()`, tests, and fire-and-forget goroutines (e.g. async billing).
- Propagate the incoming context through the entire call chain.

## Security

- **Never log secrets** — tokens, passwords, API keys. No exceptions.
- Encrypt all OAuth tokens with AES-256-GCM (`pkg/crypto`) before database storage.
- Parameterized queries only (`pgx` + `squirrel`); no `fmt.Sprintf` into SQL.
- Validate all external input with `github.com/go-playground/validator/v10`.
- Rate-limit per user and per endpoint (Redis token bucket); see `services/api/internal/middleware/`.

## Database Access

- Use `squirrel` to build SQL; it prevents injection and composes cleanly.
- Always pass `ctx` through to `pool.QueryRow(ctx, ...)`.
- Convert `pgx.ErrNoRows` to domain-specific `ErrXxxNotFound` before returning.
- Use connection pools (`*pgxpool.Pool`), not individual connections.
- Transactions: `BeginTx(ctx, ...)` + `defer tx.Rollback()` + explicit `tx.Commit()`.

## Testing

- Table-driven tests via `t.Run`.
- Mock *interfaces*, not concrete types — preferably with struct-with-func-fields.
- Test business logic, not trivial getters/setters.
- Use `testify/assert` (assertion) and `testify/require` (fatal assertion).
- Use `t.Setenv` for env vars in tests — auto-restores on cleanup. Never `os.Setenv`.
- Integration tests live in `_integration_test.go` files with a build tag, or in `test/integration/`.

## LLM Orchestrator Specifics

- **Tool naming**: `{platform}__{action}` for integration-scoped tools (e.g. `telegram__send_channel_post`); no `__` prefix for internal tools.
- **NATS subjects**: `tasks.{agentID}` (e.g. `tasks.telegram`).
- **Async billing** is fire-and-forget: `go r.logBilling(context.Background(), ...)` — the only sanctioned use of `Background()` in business code.
- **SSE streaming**: `data: <json>\n\n` with `flusher.Flush()` after each event. Event types: `text`, `tool_call`, `tool_result`, `done`, `error`.

## RPA Agent Specifics (`services/agent-yandex-business/`)

- `withRetry` — exponential backoff + canary check between attempts.
- `withPage` — acquire from pool, execute, release.
- `humanDelay` — random 500–1500 ms between actions to avoid detection.
- Screenshot-on-error written to `rpa-screenshots/` for diagnosis.
- Resilient CSS/XPath selectors with fallback strategy (DOM drifts).

## Dependencies

Approved Go packages:

- `github.com/jackc/pgx/v5` — PostgreSQL driver + pool
- `github.com/Masterminds/squirrel` — SQL builder
- `go.mongodb.org/mongo-driver/v2` — MongoDB
- `github.com/redis/go-redis/v9` — Redis
- `github.com/nats-io/nats.go` — NATS
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/golang-jwt/jwt/v5` — JWT
- `github.com/go-playground/validator/v10` — input validation
- `github.com/google/uuid` — UUIDs
- `github.com/sashabaranov/go-openai` — OpenAI + OpenRouter
- `github.com/anthropics/anthropic-sdk-go` — Anthropic (value type, not pointer; see `MEMORY.md` for version notes)
- `github.com/stretchr/testify` — assertions

Before adding a new dep, check `pkg/` first — shared utilities already cover: `pkg/crypto`, `pkg/domain`, `pkg/llm`, `pkg/a2a`, `pkg/logger`, `pkg/health`, `pkg/metrics`, `pkg/tokenclient`.

## References

- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Effective Go](https://go.dev/doc/effective_go)
