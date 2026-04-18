# Coding Best Practices — OneVoice

This document defines coding standards and best practices for the OneVoice monorepo.

## General Principles

1. **Simplicity over cleverness** — Write obvious, maintainable code
2. **Security by default** — Never log secrets, always validate input, encrypt sensitive data
3. **Fail fast** — Return errors early, avoid deep nesting
4. **Test what matters** — Focus on business logic, not trivial getters/setters
5. **Document why, not what** — Code explains what; comments explain why

---

## Go Backend Standards

### Project Structure

- **Monorepo with Go workspace** — `go.work` for cross-module development
- **Domain-driven design** — `pkg/domain/` for shared models
- **Clean architecture** — handler → service → repository layers
- **Internal packages** — Use `/internal/` to prevent external imports

### Naming Conventions

```go
// ✅ GOOD
type UserRepository interface {
    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
    Create(ctx context.Context, user *User) error
}

// ❌ BAD
type UserRepo interface {
    Get(id uuid.UUID) (*User, error)  // Missing context
    CreateUser(user *User) error       // Redundant "User" in name
}
```

**Rules:**
- Interface names: noun (e.g., `UserRepository`, not `IUserRepository`)
- Methods: verb + noun (e.g., `GetByID`, not `Get`)
- Always pass `context.Context` as first parameter
- Use `uuid.UUID` for IDs, not `string`

### Error Handling

```go
// ✅ GOOD
func (s *Service) DoSomething(ctx context.Context, id uuid.UUID) error {
    user, err := s.userRepo.GetByID(ctx, id)
    if err != nil {
        return fmt.Errorf("get user: %w", err)
    }

    if user.Status != "active" {
        return ErrUserInactive
    }

    // ... business logic
    return nil
}

// ❌ BAD
func (s *Service) DoSomething(ctx context.Context, id uuid.UUID) error {
    user, _ := s.userRepo.GetByID(ctx, id)  // ❌ Ignoring error
    if user == nil {                         // ❌ nil check instead of error
        return errors.New("user not found") // ❌ Unwrapped error
    }
    return nil
}
```

**Rules:**
- Never ignore errors (`_, _ :=` is banned except in tests)
- Wrap errors with `fmt.Errorf(..., %w, err)` for traceability
- Define sentinel errors: `var ErrUserNotFound = errors.New("user not found")`
- Use `errors.Is()` and `errors.As()` for error checking

### Context & Cancellation

```go
// ✅ GOOD
func (s *Service) Process(ctx context.Context) error {
    timer := time.NewTimer(30 * time.Second)
    defer timer.Stop()

    resultCh := make(chan Result, 1)
    errCh := make(chan error, 1)

    go func() {
        result, err := s.doWork(ctx)
        if err != nil {
            errCh <- err
            return
        }
        resultCh <- result
    }()

    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-timer.C:
        return ErrTimeout
    case err := <-errCh:
        return err
    case result := <-resultCh:
        return s.handleResult(result)
    }
}

// ❌ BAD
func (s *Service) Process(ctx context.Context) error {
    time.Sleep(30 * time.Second)  // ❌ Ignores cancellation
    return s.doWork(context.Background())  // ❌ Creates new context
}
```

**Rules:**
- Always respect `ctx.Done()` in long-running operations
- Never create `context.Background()` in business logic (only in `main()` or tests)
- Pass context through the entire call chain

### Security

```go
// ✅ GOOD
type IntegrationService struct {
    encryptor *crypto.Encryptor
}

func (s *IntegrationService) SaveToken(ctx context.Context, token string) error {
    encrypted, err := s.encryptor.Encrypt([]byte(token))
    if err != nil {
        return fmt.Errorf("encrypt token: %w", err)
    }

    return s.repo.Save(ctx, encrypted)
}

// ❌ BAD
func SaveToken(ctx context.Context, token string) error {
    log.Info("Saving token", "token", token)  // ❌ LOGS SECRET
    return repo.Save(ctx, []byte(token))      // ❌ STORES PLAINTEXT
}
```

**Rules:**
- **Never log secrets** — tokens, passwords, API keys
- Encrypt all OAuth tokens with AES-256-GCM before database storage
- Use parameterized queries (pgx handles this automatically)
- Validate all external input (use `validator` library)
- Rate limit per-user, per-endpoint (Redis token bucket)

### Database Access

```go
// ✅ GOOD (using pgx + squirrel)
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

// ❌ BAD
func GetUser(email string) (*User, error) {
    query := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email)  // ❌ SQL INJECTION
    rows, _ := db.Query(query)  // ❌ Ignored error, no context
    // ...
}
```

**Rules:**
- Use `squirrel` for building queries (type-safe, prevents injection)
- Always use `ctx` parameter for cancellation
- Convert `pgx.ErrNoRows` to domain-specific errors
- Use connection pools, not individual connections
- Transactions: `BeginTx(ctx, ...)`, always `defer tx.Rollback()`

### Testing

```go
// ✅ GOOD
func TestUserService_Create(t *testing.T) {
    ctx := context.Background()

    t.Run("success", func(t *testing.T) {
        repo := &mockUserRepo{
            createFunc: func(ctx context.Context, user *domain.User) error {
                assert.NotEqual(t, uuid.Nil, user.ID)
                return nil
            },
        }

        svc := NewUserService(repo)
        err := svc.Create(ctx, "test@example.com", "password123")
        assert.NoError(t, err)
    })

    t.Run("duplicate email", func(t *testing.T) {
        repo := &mockUserRepo{
            createFunc: func(ctx context.Context, user *domain.User) error {
                return domain.ErrUserExists
            },
        }

        svc := NewUserService(repo)
        err := svc.Create(ctx, "test@example.com", "password123")
        assert.ErrorIs(t, err, domain.ErrUserExists)
    })
}

// ❌ BAD
func TestUser(t *testing.T) {
    user := &User{Email: "test"}
    if user.Email != "test" {  // ❌ Testing trivial logic
        t.Fail()
    }
}
```

**Rules:**
- Use table-driven tests for multiple scenarios
- Mock interfaces, not concrete types
- Test business logic, not getters/setters
- Use `testify/assert` for assertions
- Integration tests: separate `_integration_test.go` files with build tag

---

## Frontend (Next.js/React) Standards

### Project Structure

```
src/
├── app/              # Next.js App Router pages
│   ├── (auth)/       # Route groups
│   └── (dashboard)/
├── components/
│   ├── ui/           # shadcn/ui primitives
│   └── feature/      # Feature-specific components
├── lib/              # Utilities
└── hooks/            # Custom hooks
```

### Component Patterns

```tsx
// ✅ GOOD
interface ChatMessageProps {
  message: Message;
  onRetry?: (id: string) => void;
}

export function ChatMessage({ message, onRetry }: ChatMessageProps) {
  const handleRetry = useCallback(() => {
    onRetry?.(message.id);
  }, [message.id, onRetry]);

  return (
    <div className="flex gap-3 p-4">
      <Avatar>{message.role}</Avatar>
      <div className="flex-1">
        <Markdown>{message.content}</Markdown>
        {message.isError && (
          <Button onClick={handleRetry} variant="ghost" size="sm">
            Retry
          </Button>
        )}
      </div>
    </div>
  );
}

// ❌ BAD
export function ChatMessage(props) {  // ❌ No types
  return (
    <div style={{ padding: "16px" }}>  {/* ❌ Inline styles instead of Tailwind */}
      <div>{props.message.content}</div>
      <button onclick={props.onRetry}>Retry</button>  {/* ❌ onclick lowercase */}
    </div>
  );
}
```

**Rules:**
- Always define TypeScript interfaces for props
- Use `function` declarations, not `const` (better for debugging)
- Use Tailwind classes, not inline styles
- Extract logic to custom hooks when > 10 lines
- Server components by default; add `"use client"` only when needed

### State Management

```tsx
// ✅ GOOD (Zustand)
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

// In component
const { user, setAuth } = useAuthStore();

// ❌ BAD
const [user, setUser] = useState();  // ❌ Global state in local useState
const [token, setToken] = useState();
```

**Rules:**
- Use Zustand for global state (auth, integrations status)
- Use React Query for server state (fetching, caching)
- Use `useState` only for local UI state (open/closed, selected tab)
- Never lift state unnecessarily — keep it close to where it's used

### Forms

```tsx
// ✅ GOOD
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

const businessSchema = z.object({
  name: z.string().min(1, "Name is required"),
  phone: z.string().regex(/^\+?\d{10,15}$/, "Invalid phone"),
  email: z.string().email("Invalid email"),
});

type BusinessFormData = z.infer<typeof businessSchema>;

export function BusinessForm() {
  const { register, handleSubmit, formState: { errors } } = useForm<BusinessFormData>({
    resolver: zodResolver(businessSchema),
  });

  const onSubmit = async (data: BusinessFormData) => {
    const response = await fetch('/api/v1/business', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });

    if (!response.ok) {
      throw new Error('Failed to save');
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <Input {...register("name")} />
      {errors.name && <p className="text-sm text-red-500">{errors.name.message}</p>}
      {/* ... */}
    </form>
  );
}

// ❌ BAD
export function BadForm() {
  const [name, setName] = useState("");

  const handleSubmit = () => {
    if (!name) {  // ❌ Manual validation
      alert("Name required");
    }
    fetch('/api/business', { body: name });  // ❌ No error handling
  };

  return <input value={name} onChange={e => setName(e.target.value)} />;
}
```

**Rules:**
- Use `react-hook-form` + `zod` for all forms
- Define schema with Zod, infer TypeScript types
- Show validation errors inline, not alerts
- Handle loading/error states explicitly

---

## API Design

### REST Conventions

```
GET    /api/v1/users/{id}         → 200 + User JSON
POST   /api/v1/users               → 201 + User JSON (Location header)
PUT    /api/v1/users/{id}          → 200 + User JSON
DELETE /api/v1/users/{id}          → 204 (no body)

GET    /api/v1/businesses/{id}/integrations  → 200 + Integration[]
POST   /api/v1/integrations/{platform}/connect → 302 redirect to OAuth
```

**Rules:**
- Version API (`/api/v1/`)
- Use plural nouns (`/users`, not `/user`)
- Use HTTP verbs correctly (GET = read-only, POST = create, PUT = update, DELETE = delete)
- Return proper status codes (200, 201, 204, 400, 401, 403, 404, 500)
- JSON responses: `{"data": {...}, "error": null}` or `{"data": null, "error": "..."}`

### Error Responses

```json
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "Validation failed",
    "details": {
      "email": "Invalid email format",
      "phone": "Phone number required"
    }
  }
}
```

**Status codes:**
- `400` — Bad request (validation error)
- `401` — Unauthorized (no token or expired)
- `403` — Forbidden (valid token, insufficient permissions)
- `404` — Not found
- `409` — Conflict (e.g., duplicate email)
- `429` — Rate limit exceeded
- `500` — Internal server error (log but don't expose details)

---

## Git Workflow

### Commit Messages

```
feat: add Google Business Profile integration
fix: prevent token refresh race condition
refactor: extract JWT logic to pkg/auth
docs: add API authentication guide
test: add unit tests for orchestrator
chore: update dependencies
```

**Format:** `<type>: <subject>` (lowercase, no period)

**Types:** `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`

### Branch Naming

- `main` — production-ready code
- `develop` — integration branch (if using Gitflow)
- `feat/google-integration` — new feature
- `fix/token-refresh-bug` — bug fix
- `refactor/auth-service` — refactoring

---

## Security Checklist

Before deployment, verify:

- [ ] All secrets in environment variables (never in code)
- [ ] OAuth tokens encrypted with AES-256-GCM
- [ ] Rate limiting enabled (Redis)
- [ ] CORS configured (whitelist only)
- [ ] HTTPS enforced (HTTP → HTTPS redirect)
- [ ] Input validation on all endpoints
- [ ] SQL injection impossible (parameterized queries only)
- [ ] XSS prevention (React escapes by default, but validate in API)
- [ ] CSRF tokens for state-changing operations
- [ ] Audit logs for sensitive actions (token access, deletion, etc.)

---

## Performance Guidelines

### Backend

- Use connection pools (PostgreSQL, MongoDB, Redis)
- Cache expensive queries (Redis, 5-60 min TTL)
- Paginate list endpoints (default 20, max 100)
- Use indexes on frequently queried fields
- Stream large responses (SSE for chat, chunked for file downloads)

### Frontend

- Use Next.js Image component for images (automatic optimization)
- Lazy load routes with `dynamic()` + `{ ssr: false }`
- Debounce search inputs (300ms)
- Virtualize long lists (`react-window`)
- Minimize bundle size (check with `npm run build`)

---

## Dependencies

### Go

- `github.com/jackc/pgx/v5` — PostgreSQL driver
- `github.com/Masterminds/squirrel` — SQL builder
- `go.mongodb.org/mongo-driver/v2` — MongoDB
- `github.com/redis/go-redis/v9` — Redis
- `github.com/nats-io/nats.go` — NATS
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/golang-jwt/jwt/v5` — JWT
- `github.com/go-playground/validator/v10` — Validation
- `github.com/sashabaranov/go-openai` — OpenAI
- `github.com/anthropics/anthropic-sdk-go` — Anthropic

### Frontend

- `next@14` — Framework
- `react@18` — UI library
- `tailwindcss@3` — Styling
- `shadcn/ui` — Component library
- `zustand` — State management
- `react-hook-form` — Forms
- `zod` — Validation
- `@tanstack/react-query` — Server state (fetching, caching)

---

## Documentation

Current docs:

- `README.md` — setup instructions, architecture overview
- `docs/architecture.md` — system diagrams, module map, data flow
- `docs/golden-principles.md` — top-level rules enforced by linters/CI
- `docs/patterns.md` — approved code patterns with examples
- `docs/anti-patterns.md` — mistakes to avoid
- `pkg/CLAUDE.md` + per-service `services/*/CLAUDE.md` — scoped module guidance

Inline code comments should explain:
- **Why** decisions were made (not what the code does)
- Complex algorithms or non-obvious logic
- Security considerations (why token is encrypted here, why rate limit is X)

---

## Review Checklist

Before merging:

- [ ] Tests pass (`make test`)
- [ ] Lint passes (`make lint`)
- [ ] No secrets in code or logs
- [ ] Errors wrapped with context
- [ ] API endpoints documented
- [ ] Database migrations included (if schema changed)
- [ ] Breaking changes noted in PR description

---

## References

- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
- [Effective Go](https://go.dev/doc/effective_go)
- [React Best Practices 2024](https://react.dev/learn)
- [Next.js Documentation](https://nextjs.org/docs)
