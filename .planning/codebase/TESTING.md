# OneVoice Testing Patterns

This document describes testing strategies, patterns, and tools used in the OneVoice codebase.

## Go Testing Overview

### Test Framework

**Standard library:** `testing` package with `*testing.T`

**Assertion library:** `github.com/stretchr/testify/assert` and `require`
- `assert` — non-fatal assertions (test continues on failure)
- `require` — fatal assertions (test stops on failure)

**Mocking:** `github.com/stretchr/testify/mock`
- Mock.Mock struct for recording expected calls
- `m.On("Method", args...).Return(result)`
- `m.AssertExpectations(t)` to verify all expected calls were made

### Test File Organization

**File naming:** `{name}_test.go` in the same package as the code being tested

**Test discovery:** All `*_test.go` files in a package, functions matching `Test*` or `Benchmark*`

**Location patterns:**
- Unit tests: `services/{service}/internal/{package}/{name}_test.go`
- Integration tests: `test/integration/{scenario}_test.go`
- Example: `services/api/internal/handler/auth_test.go`

### Running Tests

**Go module tests:**
```bash
# Single module (from module directory)
cd services/api && GOWORK=off go test -race ./...

# All modules (from root)
make test

# All (Go + frontend)
make test-all

# With coverage
make test-coverage

# Integration tests with Docker
make test-integration
```

See: `Makefile` (lines 34-72)

## Unit Testing Patterns

### Basic Test Structure

```go
func TestAuthHandler_Login(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name:        "successful login",
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Login", mock.Anything, "user@example.com", "password123").
					Return(&domain.User{...}, "access-token", "refresh-token", nil)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body string) {
				var response map[string]interface{}
				err := json.Unmarshal([]byte(body), &response)
				require.NoError(t, err)
				assert.Equal(t, "access-token", response["accessToken"])
			},
		},
		// More test cases...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test implementation
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
				bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Login(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())
			mockService.AssertExpectations(t)
		})
	}
}
```

**Key elements:**
- Table-driven tests: `tests := []struct{...}{...}`
- Each test case has: name, inputs, mock setup function, expected output, verification logic
- Sub-test per case: `t.Run(tt.name, ...)`
- Mock setup via callback function for flexibility

See: `services/api/internal/handler/auth_test.go` (lines 69-204)

### Mocking Interfaces

**Defining Mock:**
```go
type MockUserService struct {
	mock.Mock
}

func (m *MockUserService) Login(ctx context.Context, email, password string) (
	user *domain.User, accessToken, refreshToken string, err error) {
	args := m.Called(ctx, email, password)
	if args.Get(0) == nil {
		return nil, "", "", args.Error(3)
	}
	return args.Get(0).(*domain.User), args.String(1), args.String(2), args.Error(3)
}
```

**Setting up expectations:**
```go
mockService := new(MockUserService)
mockService.On("Login", mock.Anything, "user@example.com", "password").
	Return(&domain.User{ID: uuid.New()}, "token", nil)

// Call the code being tested
handler.Login(w, req)

// Verify
mockService.AssertExpectations(t)
```

**Using `mock.Anything`:**
- `mock.Anything` matches any argument (useful for context, UUIDs)
- Specific values for important assertions: "user@example.com"

See: `services/api/internal/handler/auth_test.go` (lines 22-67)

### HTTP Handler Testing

Use `httptest` package:

```go
t.Run("invalid json", func(t *testing.T) {
	mockService := new(MockUserService)

	handler := NewAuthHandler(mockService)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{invalid json}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Login(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"error"`)
})
```

**Components:**
- `httptest.NewRequest(method, url, body)` — creates *http.Request
- `httptest.NewRecorder()` — captures response
- `w.Code` — HTTP status code
- `w.Body.String()` — response body

### Context Handling in Tests

```go
t.Run("missing user ID in context", func(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	mockService := new(MockUserService)

	handler := NewAuthHandler(mockService)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)

	// Setup context with user ID
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.Me(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
})
```

Use `context.WithValue()` to inject test values (auth user ID, business ID, etc.)

See: `services/api/internal/handler/auth_test.go` (lines 521-620)

### Assertion Patterns

```go
// Using testify/assert
assert.Equal(t, expected, actual)
assert.NoError(t, err)
assert.NotEmpty(t, value)
assert.Contains(t, containerString, substring)
assert.NotContains(t, body, "database")  // Prevent info leakage in error responses

// Using testify/require (fatal)
require.NoError(t, err)
require.NotNil(t, result)

// JSON unmarshaling in tests
var response map[string]interface{}
err := json.Unmarshal([]byte(body), &response)
require.NoError(t, err)
assert.Equal(t, "value", response["key"])
```

### Test Error Cases

Standard patterns:
- **Invalid input:** missing required fields, invalid format
- **Not found:** entity doesn't exist (404)
- **Conflict:** duplicate entry (409)
- **Unauthorized:** missing or invalid credentials (401)
- **Forbidden:** authenticated but no permission (403)
- **Internal server error:** unexpected failures (500)

Example:
```go
{
	name:        "user not found",
	requestBody: `{}`,
	mockSetup: func(m *MockUserService) {
		m.On("GetByID", mock.Anything, testUserID).
			Return(nil, domain.ErrUserNotFound)
	},
	wantStatus: http.StatusNotFound,
	checkResponse: func(t *testing.T, body string) {
		assert.Contains(t, body, `"error":"user not found"`)
	},
},
```

See: `services/api/internal/handler/auth_test.go` (lines 557-570)

## Integration Tests

### Setup Pattern

File: `test/integration/main_test.go`

```go
var (
	baseURL     string
	httpClient  *http.Client
	pgPool      *pgxpool.Pool
	mongoDB     *mongo.Database
	redisClient *redis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Get test environment URLs
	baseURL = os.Getenv("TEST_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	// Wait for API to be ready
	if err := waitForAPI(baseURL, 30*time.Second); err != nil {
		fmt.Printf("API not ready: %v\n", err)
		os.Exit(1)
	}

	// Connect to databases
	pgURL := os.Getenv("TEST_POSTGRES_URL")
	if pgURL != "" {
		pgPool, _ = pgxpool.New(ctx, pgURL)
	}

	mongoURL := os.Getenv("TEST_MONGO_URL")
	if mongoURL != "" {
		mongoClient, _ := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
		mongoDB = mongoClient.Database("onevoice_test")
	}

	// HTTP client
	httpClient = &http.Client{Timeout: 10 * time.Second}

	// Run tests
	code := m.Run()

	// Cleanup
	if pgPool != nil {
		pgPool.Close()
	}
	if redisClient != nil {
		redisClient.Close()
	}

	os.Exit(code)
}

func cleanupDatabase(t *testing.T) {
	ctx := context.Background()
	if pgPool != nil {
		pgPool.Exec(ctx, "TRUNCATE users, businesses, integrations CASCADE")
	}
	if mongoDB != nil {
		mongoDB.Collection("conversations").Drop(ctx)
		mongoDB.Collection("messages").Drop(ctx)
	}
	if redisClient != nil {
		redisClient.FlushDB(ctx)
	}
}
```

**Key points:**
- `TestMain()` runs before all tests (setup)
- Use `os.Getenv()` to configure from Docker environment
- Call `cleanupDatabase()` in each test to reset state
- Defer cleanup of connections

See: `test/integration/main_test.go` (lines 25-121)

### Integration Test Pattern

```go
func TestAuthFlow(t *testing.T) {
	cleanupDatabase(t)

	var accessToken, refreshToken string
	var userID string

	t.Run("Register", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/register",
			"application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.NotEmpty(t, result["id"])
		userID = result["id"].(string)
	})

	t.Run("Login", func(t *testing.T) {
		payload := map[string]string{
			"email":    "test@example.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)

		resp, err := httpClient.Post(baseURL+"/api/v1/auth/login",
			"application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		accessToken = result["accessToken"].(string)
		refreshToken = result["refreshToken"].(string)
	})

	t.Run("Me", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
```

**Pattern:**
- Use sub-tests (`t.Run()`) to sequence operations
- Share state (tokens, IDs) via outer scope variables
- Each sub-test is a logical step in the flow
- Real API calls through HTTP

See: `test/integration/auth_test.go` (lines 13-120)

### Environment Variables

Integration tests read from environment:
```bash
TEST_API_URL=http://localhost:8081          # Running API endpoint
TEST_POSTGRES_URL=postgres://...           # Test database connection
TEST_MONGO_URL=mongodb://localhost:27018   # Test MongoDB
TEST_REDIS_URL=localhost:6380               # Test Redis
```

Set these in Docker Compose test environment or CI/CD.

See: `Makefile` (lines 54-72)

### Docker Compose Test Setup

File: `test/integration/docker-compose.test.yml` (implied by Makefile)

Starts isolated test infrastructure:
- PostgreSQL on port 5433 (separate from dev 5432)
- MongoDB on port 27018 (separate from dev 27017)
- Redis on port 6380 (separate from dev 6379)
- API service pointing to test databases

Run: `make test-integration`

## Repository Tests

Most repository tests are placeholders (`assert.True(t, true)`), as repositories are tested through integration tests or mocked in unit tests of services.

Example: `services/api/internal/repository/user_test.go`
```go
func TestUserRepository(t *testing.T) {
	// Placeholder test to verify compilation
	assert.True(t, true, "Basic test passes")
}
```

**Why:** Repositories access actual databases, which is integration-level testing. Unit tests mock the repository interface instead.

## Environment Variable Testing

Use `t.Setenv()` (Go 1.17+) for test-isolated environment changes:

```go
func TestConfigLoading(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-that-is-32-bytes-long!")
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "test-secret-that-is-32-bytes-long!", cfg.JWTSecret)
}
```

**Benefits:**
- `t.Setenv()` auto-restores the environment after the test
- No pollution of other tests
- Cleaner than `os.Setenv()` + manual restoration

## Frontend Testing (Vitest)

### Configuration

**Files:**
- `services/frontend/vitest.config.ts` — Vitest configuration
- `services/frontend/vitest.setup.ts` — Setup file (Jest environment, globals)

### Test Patterns

Pure utility functions are tested before integration:

```typescript
// From useChat.ts - exported for testing
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null;
  try {
    return JSON.parse(line.slice(6));
  } catch {
    return null;
  }
}

export function applySSEEvent(msg: Message, event: Record<string, unknown>): Message {
  const type = event.type as string;
  if (type === 'text') {
    return { ...msg, content: msg.content + (event.content as string) };
  }
  // Handle other event types...
  return msg;
}
```

These utilities can be unit-tested independently before testing the full hook.

See: `services/frontend/hooks/useChat.ts` (lines 6-56)

### Running Frontend Tests

```bash
# From frontend directory
cd services/frontend && pnpm test

# Watch mode
pnpm test --watch

# Coverage
pnpm test --coverage
```

## Continuous Integration

### Linting CI

All PRs must pass:
```bash
make lint          # Go linters
make lint-frontend # Frontend linters
```

If linting fails, fix locally with `make fmt-fix` and push.

### Test CI

All PRs must pass:
```bash
make test       # Go unit tests with -race flag
make test-all   # Go + frontend tests
```

Integration tests optional in CI (may run separately in scheduled jobs).

### Make Targets for Quality

```bash
make test                    # Go tests with race detector
make test-all                # Go + frontend tests
make test-coverage           # Coverage report
make test-integration        # Docker-based integration tests
make lint                    # Go linting
make lint-frontend           # Frontend linting
make lint-all                # Both
make fmt-fix                 # Auto-format everything
```

See: `Makefile` (lines 1-90)

## Test Error Patterns

### Common Failure Patterns

**1. Context timeout in service tests**
```
// Issue: service checks ctx.Err() first
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
defer cancel()
time.Sleep(2*time.Second)  // ❌ Exceeds timeout
```

**Solution:** Use `context.Background()` in unit tests, test timeout behavior separately

**2. Mock not called**
```
// ❌ Mock was defined but test took different code path
mockService.On("Register", ...).Return(...)
mockService.AssertExpectations(t)  // Fails if Register was never called
```

**Solution:** Check code flow, verify mock setup matches actual behavior

**3. Database test pollution**
```
// ❌ Previous test didn't clean up
func TestX(t *testing.T) {
	cleanupDatabase(t)  // ✅ Always clean first
}
```

**4. Race conditions in tests**
```bash
go test -race ./...  # Detects concurrent map access, etc.
```

Most race conditions caught automatically by Go test runner.

## Summary

**Testing hierarchy:**
1. **Unit tests** — Mock all dependencies, test single functions/methods
2. **Integration tests** — Real API + databases, test complete flows
3. **E2E tests** — User-facing scenarios (optional, via Playwright)

**Key tools:**
- Testing: `testing` package
- Assertions: `testify/assert`, `testify/require`
- Mocking: `testify/mock`
- HTTP: `httptest`
- Databases: Docker Compose (for integration tests)

**Running tests:**
- `make test` — Go unit tests
- `make test-all` — Go + frontend
- `make test-integration` — Docker-based integration tests
- `make lint-all` — All quality checks

**Best practices:**
- Table-driven tests for multiple cases
- Mock interfaces at consumption point
- Use `cleanupDatabase()` in integration tests
- Test error cases (validation, not found, conflicts, etc.)
- Avoid test pollution (reset state between tests)
- Use `t.Setenv()` for environment isolation
- Use `-race` flag to catch concurrency issues
