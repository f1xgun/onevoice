# OneVoice Coding Conventions

This document describes the coding conventions and architectural patterns used throughout the OneVoice codebase.

## Go Code Style

### Package Organization

**Location Pattern:**
- Shared code: `pkg/{domain,a2a,llm,crypto,logger}`
- Service code: `services/{service-name}/internal/{package}`
- Entry point: `services/{service-name}/cmd/main.go`

**Package Naming:**
- Lowercase, single-word when possible
- Use full English names (e.g., `handler`, `repository`, not `h`, `repo`)
- Group related functionality: `handler`, `service`, `repository`, `middleware`, `config`, `router`

**Import Organization** (checked by golangci-lint):
- Standard library imports first
- Third-party imports second
- Internal/local imports last (`github.com/f1xgun/onevoice/...`)
- Use `goimports` auto-formatter to organize imports
- All services use: `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in go.mod

See: `.golangci.yml` formatters.enable=[gofmt, goimports]

### Function and Method Signatures

**Constructor Pattern:**
```go
// Exported constructors: NewXxx(dependencies) Interface
func NewAuthHandler(userService UserService) *AuthHandler {
	if userService == nil {
		panic("userService cannot be nil")  // Fail fast on initialization
	}
	return &AuthHandler{userService: userService}
}

// Compile-time interface check (in service)
var _ BusinessService = (*businessService)(nil)
```

**Method Receivers:**
- Use pointer receivers `(s *serviceName)` for methods that might modify state or are part of an interface
- Use value receivers only for small, immutable helper types

**Error Handling Pattern:**
```go
// Always wrap errors with fmt.Errorf("context: %w", err)
if err != nil {
	return nil, fmt.Errorf("fetch user: %w", err)
}

// Map database errors to domain sentinel errors
if errors.Is(err, pgx.ErrNoRows) {
	return nil, domain.ErrUserNotFound
}

// Check context first in all methods
if err := ctx.Err(); err != nil {
	return nil, err
}
```

See: `services/api/internal/service/business.go` for patterns

### Naming Conventions

**Constants:**
- UPPER_SNAKE_CASE only for truly constant values
- Most configuration uses environment variables via config.go

**Variables:**
- camelCase for local variables
- Exported: PascalCase
- Unexported: camelCase
- Singular names: prefer `user` over `users` for single values

**Interfaces:**
- Verb-oriented: `UserService`, `BusinessRepository`, `LLMProvider`
- End in `-er` for single methods: `Requester`, `Executor`
- Service interfaces exported in `services/{service}/internal/handler` or `service`

**Files:**
- snake_case: `auth_test.go`, `chat_proxy.go`
- One main type per file (or group related small types)
- Test files: `{name}_test.go` in same package

See: `services/api/internal/handler/auth.go`, `services/api/internal/service/business.go`

## Architecture Patterns

### Layered Architecture: Handler → Service → Repository

**Handler Layer** (`internal/handler/`)
- Parse HTTP requests using `http.Request` and `json.NewDecoder`
- Validate input with `github.com/go-playground/validator/v10`
- Call service layer methods
- Format responses using `writeJSON()`, `writeJSONError()`, `writeValidationError()`
- Never access database or external services directly

**Service Layer** (`internal/service/`)
- Business logic and validation
- Orchestrate repositories and other services
- Use `fmt.Errorf("context: %w", err)` for all error wrapping
- Return domain types, not database types
- No HTTP concepts (status codes, headers)

**Repository Layer** (`internal/repository/`)
- SQL queries only (use `squirrel` for PostgreSQL)
- Convert database errors to domain sentinel errors
- Manage transactions where needed
- No business logic

See: `services/api/cmd/main.go` (lines 91-107) for wiring pattern

### Configuration Pattern

**Config Struct:**
```go
type Config struct {
	Port          string
	PostgresHost  string
	JWTSecret     string
	// All fields as strings or primitives; validation in Load()
}

func Load() (*Config, error) {
	cfg := &Config{
		Port: getEnv("PORT", "8080"),
		// ...
	}
	// Validate required fields
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	return cfg, nil
}
```

Helper functions:
- `getEnv(key, defaultValue string) string` — read with fallback
- `getEnvInt(key string, defaultValue int) int` — parse as int with fallback

See: `services/api/internal/config/config.go`

### Service Interfaces

Define interfaces where they're used, not where they're implemented:
```go
// In handler package (where it's consumed)
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
}

// In service package (where it's implemented)
type userService struct {
	repo domain.UserRepository
}

func NewUserService(repo domain.UserRepository) UserService {
	return &userService{repo: repo}
}
```

See: `services/api/internal/handler/auth.go` (lines 18-25)

### Request/Response Types

Define request/response types near their handlers:
```go
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type LoginResponse struct {
	User         *domain.User `json:"user"`
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
}
```

JSON field names: camelCase (matches frontend conventions)
Database field names: snake_case (by struct tag `db:"snake_case"`)

See: `services/api/internal/handler/auth.go` (lines 47-87)

### Error Responses

Structured error responses prevent information leakage:
```go
type ErrorResponse struct {
	Error string `json:"error"`
}

type ValidationErrorResponse struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields"`
}

// Usage in handler
if err != nil {
	slog.Error("internal operation failed", "error", err)  // Log internals
	writeJSONError(w, http.StatusInternalServerError, "internal server error")  // Send generic message
}
```

See: `services/api/internal/handler/response.go`

## Shared Domain Models

**Location:** `pkg/domain/`

**Model Files:**
- `models.go` — PostgreSQL entities (User, Business, Integration)
- `mongo_models.go` — MongoDB entities (Conversation, Message)
- `repository.go` — Repository interface definitions
- `errors.go` — Sentinel errors (var ErrXxx = errors.New("..."))
- `roles.go` — Role constants

**JSON Tags:**
- Field names: PascalCase on struct, camelCase in JSON
- Use `json:"-"` for sensitive fields (PasswordHash, EncryptedTokens)

See: `pkg/domain/models.go` (lines 9-16 show the pattern)

## Frontend Code Style

### TypeScript/React Patterns

**File Organization:**
- Components: `components/` (UI components only)
- Feature pages: `app/(dashboard)/` (route groups)
- Hooks: `hooks/useXxx.ts` (custom hooks, pure logic)
- Utilities: `lib/` (API clients, constants, helpers)
- Types: `types/` (exported type definitions)
- Stores: `lib/auth.ts`, `lib/api.ts` (Zustand stores)

**Component Declaration:**
```tsx
// Use function declarations, not arrow functions
function ComponentName({ prop1, prop2 }: ComponentProps) {
  return <div>{prop1}</div>;
}

interface ComponentProps {
  prop1: string;
  prop2?: number;
}
```

**Import Strategy:**
- `import type { ... }` for type-only imports
- Group imports: React → External packages → Internal utilities/types
- Use relative imports for local files

See: `services/frontend/components/sidebar.tsx` (lines 1-22)

**Client Components:**
```tsx
'use client';  // At the top of file if using hooks/events

import { useState } from 'react';  // Move to top

function Component() {
  const [state, setState] = useState<Type>('initial');
}
```

Server components by default; add `'use client'` only when necessary.

### Form Patterns

Always use `react-hook-form` + `zod` for forms:
```tsx
const form = useForm<LoginInput>({
  resolver: zodResolver(loginSchema),
  defaultValues: { email: '', password: '' },
});

function onSubmit(data: LoginInput) {
  // Handle form submission
}
```

Define schemas in `lib/schemas.ts`:
```typescript
export const loginSchema = z.object({
  email: z.string().email('Invalid email').max(254),
  password: z.string().min(6, 'Minimum 6 characters'),
});

export type LoginInput = z.infer<typeof loginSchema>;
```

See: `services/frontend/lib/schemas.ts`

### Type Definitions

Keep types colocated:
- Request/response types: `lib/api.ts` or inline handlers
- Domain types: `types/{feature}.ts`
- UI-only types: in component files (interfaces used only by one component)

See: `services/frontend/types/chat.ts`

### Styling

- **Tailwind CSS only** — no inline styles, no CSS modules
- Use `cn()` helper from `lib/utils.ts` to merge classes
- shadcn/ui primitives in `components/ui/` — don't edit manually

### API Client

Centralized Axios instance in `lib/api.ts`:
```typescript
export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
});

// Interceptors for auth token injection and 401 handling
api.interceptors.request.use(...);
api.interceptors.response.use(...);
```

See: `services/frontend/lib/api.ts` (lines 5-85)

### State Management

- **Global state:** Zustand stores in `lib/` (auth, integrations)
- **Server state:** React Query for API data (useQuery, useMutation)
- **Local UI state:** `useState` only for component-local state

## Tool Naming Convention

Platform-specific tools follow: `{platform}__{action}`

Examples:
- `telegram__send_channel_post`
- `telegram__send_channel_photo`
- `vk__create_post`
- `yandex_business__get_reviews`

This naming allows:
1. NATS subject routing: `tasks.{platform}`
2. Platform extraction: split on `__` then extract second part
3. LLM tool descriptions to remain platform-aware

See: `services/orchestrator/internal/tools/registry.go`

## Common Code Patterns

### Retry Logic in RPA Agents

```go
func withRetry(operation func() error, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := operation(); err == nil {
			return nil
		}
		time.Sleep(time.Second * time.Duration(math.Pow(2, float64(i))))
	}
	return fmt.Errorf("operation failed after %d retries", maxRetries)
}

// Usage
if err := withRetry(func() error {
	// Browser operation
}, 3); err != nil {
	return fmt.Errorf("navigate to page: %w", err)
}
```

### JSON Encoding in Handlers

Use `json.NewDecoder` for request bodies, `json.NewEncoder` for responses:
```go
var req RegisterRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
	writeJSONError(w, http.StatusBadRequest, "invalid request body")
	return
}

writeJSON(w, http.StatusOK, response)
```

### HTTP Response Writing

```go
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil && status != http.StatusNoContent {
		json.NewEncoder(w).Encode(data)
	}
}
```

See: `services/api/internal/handler/response.go`

### Query Building with Squirrel

```go
sql, args, err := r.sb.
	Select("id", "name", "email").
	From("users").
	Where(squirrel.Eq{"id": userID}).
	ToSql()
if err != nil {
	return nil, fmt.Errorf("build select: %w", err)
}

err = r.pool.QueryRow(ctx, sql, args...).Scan(&user.ID, &user.Name, &user.Email)
```

Pattern:
1. Build query with `r.sb` (StatementBuilder with `squirrel.Dollar` placeholder format)
2. Convert to SQL with `.ToSql()`
3. Check for builder errors
4. Execute with pgx connection pool

See: `services/api/internal/repository/business.go` (lines 38-45)

## Go Testing Conventions

### Unit Tests

File: `{name}_test.go` in same package

```go
func TestRegister(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		mockSetup func(*MockService)
		want      string
		wantErr   bool
	}{
		{
			name:  "success case",
			input: "valid input",
			mockSetup: func(m *MockService) {
				m.On("Method", mock.Anything).Return(nil)
			},
			want:    "expected",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test implementation
		})
	}
}
```

See: `services/api/internal/handler/auth_test.go`

### Mocking Pattern

Use `github.com/stretchr/testify/mock`:

```go
type MockUserService struct {
	mock.Mock
}

func (m *MockUserService) Login(ctx context.Context, email, password string) (*domain.User, string, string, error) {
	args := m.Called(ctx, email, password)
	if args.Get(0) == nil {
		return nil, "", "", args.Error(3)
	}
	return args.Get(0).(*domain.User), args.String(1), args.String(2), args.Error(3)
}

// In test
mockService := new(MockUserService)
mockService.On("Login", mock.Anything, "user@example.com", "pass").
	Return(&domain.User{...}, "token", nil)
```

See: `services/api/internal/handler/auth_test.go` (lines 22-67)

### HTTP Handler Testing

Use `httptest`:

```go
t.Run("success", func(t *testing.T) {
	handler := NewAuthHandler(mockService)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"email":"test@example.com","password":"pass"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockService.AssertExpectations(t)
})
```

See: `services/api/internal/handler/auth_test.go` (lines 206-323)

### Integration Tests

File: `test/integration/` (outside service packages)

```go
func TestAuthFlow(t *testing.T) {
	cleanupDatabase(t)

	t.Run("Register", func(t *testing.T) {
		payload := map[string]string{"email": "test@example.com", "password": "pass"}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/register",
			"application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}
```

Setup in `main_test.go`:
- Connect to test databases (PostgreSQL, MongoDB, Redis)
- Wait for API readiness
- Provide cleanup function

See: `test/integration/main_test.go`

## Frontend Testing Conventions

### Test Framework

- **Vitest** for unit tests (configured in `vitest.config.ts`)
- **React Testing Library** for component tests
- **Playwright** for E2E tests (configured via MCP)

Configuration: `services/frontend/vitest.config.ts`, `services/frontend/vitest.setup.ts`

### Test Organization

Tests are co-located or optional (framework allows both approaches). Most features in this project use vitest for unit tests of hooks and utilities.

Example hook test helper:
```typescript
// From useChat.ts
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null;
  try {
    return JSON.parse(line.slice(6));
  } catch {
    return null;
  }
}
```

Exported test helper functions allow testing pure logic before integration.

See: `services/frontend/hooks/useChat.ts` (lines 6-13)

## Linting and Quality

### Go Linting

Configuration: `.golangci.yml`

Enabled linters:
- Core: errcheck, govet, staticcheck, ineffassign, unused
- Style: revive, misspell, unconvert, whitespace
- Quality: gocritic, nolintlint, prealloc, unparam
- Security: bodyclose, gosec
- Correctness: exhaustive, copyloopvar

Run: `make lint` or `golangci-lint run --config .golangci.yml ./...`

### Frontend Linting

ESLint config: `.eslintrc.json`
- Extends: next/core-web-vitals, next/typescript, prettier
- Rules: consistent-type-imports, unused-vars (with _ prefix exception)

Prettier: Auto-format on save (configured in editor)

Run: `make lint-frontend` or `pnpm lint`

### All Linting Commands

```bash
make lint           # Go linting all modules
make lint-frontend  # Frontend (ESLint + Prettier check)
make lint-all       # Both Go and frontend
make fmt-fix        # Auto-format everything
```

## Summary

The codebase follows these core principles:
1. **Layered architecture:** Handler → Service → Repository (no skipping layers)
2. **Error handling:** Always wrap with fmt.Errorf("context: %w", err)
3. **Interfaces:** Define where consumed, implement in service packages
4. **Configuration:** Environment-based, validated at startup
5. **Testing:** Unit tests with mocks, integration tests for cross-layer scenarios
6. **Naming:** Clear, descriptive, following Go idioms (camelCase variables, PascalCase exports)
7. **Code organization:** Package-based grouping, one main type per file when possible
