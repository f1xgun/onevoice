# HTTP Middleware Layer

This package provides HTTP middleware components for the OneVoice API server using the chi router pattern.

## Middleware Components

### 1. Authentication Middleware (`auth.go`)

JWT-based authentication middleware that validates bearer tokens and populates request context with user claims.

**Usage:**
```go
router.Use(middleware.Auth(jwtSecret))
```

**Features:**
- Validates JWT signature using HS256
- Checks token expiration automatically
- Extracts and validates required claims: `user_id`, `email`, `role`
- Stores claims in request context for handlers to access
- Returns 401 Unauthorized for invalid/missing/expired tokens

**Context Helpers:**
```go
userID, err := middleware.GetUserID(r.Context())
email, err := middleware.GetUserEmail(r.Context())
role, err := middleware.GetUserRole(r.Context())
```

**Selective Application:**
```go
// Public routes (no auth)
router.Post("/api/v1/auth/login", loginHandler)
router.Post("/api/v1/auth/register", registerHandler)

// Protected routes (auth required)
router.Group(func(r chi.Router) {
    r.Use(middleware.Auth(jwtSecret))
    r.Get("/api/v1/businesses", listBusinessesHandler)
    r.Post("/api/v1/businesses", createBusinessHandler)
})
```

### 2. CORS Middleware (`cors.go`)

Cross-Origin Resource Sharing middleware with configurable allowed origins.

**Usage:**
```go
allowedOrigins := []string{"http://localhost:3000", "http://localhost:3001"}
router.Use(middleware.CORS(allowedOrigins))
```

**Features:**
- Validates origin against allowed list (case-insensitive)
- Handles preflight OPTIONS requests
- Sets appropriate CORS headers
- Supports wildcard `*` for development

**Headers Set:**
- `Access-Control-Allow-Origin`
- `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Authorization, Content-Type`
- `Access-Control-Max-Age: 300`

### 3. Rate Limiting Middleware (`ratelimit.go`)

Redis-based distributed rate limiting using token bucket algorithm.

**Usage:**
```go
limit := 60 // requests
window := time.Minute

router.Use(middleware.RateLimit(redisClient, limit, window))
```

**Features:**
- Per-IP rate limiting (extracts from X-Forwarded-For, X-Real-IP, or RemoteAddr)
- Per-endpoint isolation (separate limits for each path)
- Distributed across instances using Redis
- Returns 429 Too Many Requests when limit exceeded
- Sets rate limit headers on all responses

**Rate Limit Headers:**
- `X-RateLimit-Limit: 60` - Maximum requests allowed
- `X-RateLimit-Remaining: 45` - Requests remaining in window
- `X-RateLimit-Reset: 1234567890` - Unix timestamp when window resets

**Default Values:**
```go
const (
    DefaultRateLimit = 60        // 60 requests
    DefaultWindow    = time.Minute  // per minute
)
```

**Per-Endpoint Configuration:**
```go
// Stricter limits for auth endpoints
authRouter.Use(middleware.RateLimit(redis, 10, 5*time.Minute))

// Standard limits for general API
apiRouter.Use(middleware.RateLimit(redis, 60, time.Minute))
```

### 4. Request Logging Middleware (`logging.go`)

Structured request logging using Go's `log/slog` package.

**Usage:**
```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
router.Use(middleware.RequestLogger(logger))
```

**Features:**
- Logs request start and completion
- Captures status code and duration
- Uses structured JSON logging
- Wraps ResponseWriter to track status

**Log Fields:**
- `method` - HTTP method (GET, POST, etc.)
- `path` - Request path (/api/v1/businesses)
- `status` - HTTP status code (200, 401, etc.)
- `duration_ms` - Request duration in milliseconds
- `remote_addr` - Client IP address

**Example Output:**
```json
{"time":"2024-01-15T10:00:00Z","level":"INFO","msg":"request started","method":"GET","path":"/api/v1/businesses","remote_addr":"192.168.1.1:12345"}
{"time":"2024-01-15T10:00:00Z","level":"INFO","msg":"request completed","method":"GET","path":"/api/v1/businesses","status":200,"duration_ms":45,"remote_addr":"192.168.1.1:12345"}
```

## Error Response Format

All middleware use a consistent JSON error response format:

```go
type ErrorResponse struct {
    Error string `json:"error"`
}
```

**Example:**
```json
{
  "error": "missing authorization header"
}
```

## Middleware Chain Order

Apply middleware in this order for optimal behavior:

```go
router.Use(middleware.RequestLogger(logger))      // 1. Logging (first to capture everything)
router.Use(middleware.CORS(allowedOrigins))        // 2. CORS (before auth for preflight)
router.Use(middleware.RateLimit(redis, 60, time.Minute)) // 3. Rate limit (before auth to prevent brute force)
router.Use(middleware.Auth(jwtSecret))             // 4. Auth (last, only on protected routes)
```

## Testing

All middleware components have comprehensive test coverage using:
- `httptest` for HTTP testing
- `miniredis` for Redis testing (rate limit)
- `testify/assert` and `testify/require` for assertions

Run tests:
```bash
go test ./services/api/internal/middleware -v
```

## Implementation Notes

### Auth Middleware
- Uses `golang-jwt/jwt/v5` for token parsing
- Validates signing method to prevent algorithm confusion attacks
- Stores `uuid.UUID` in context (not string) for type safety

### CORS Middleware
- Case-insensitive origin matching
- Preflight requests return 204 No Content
- Supports wildcard `*` for local development (avoid in production)

### Rate Limit Middleware
- Uses Redis INCR + EXPIRE for atomic operations
- Key format: `ratelimit:{ip}:{path}`
- Fails open (allows request) if Redis is unavailable
- X-Forwarded-For takes priority over X-Real-IP and RemoteAddr

### Logging Middleware
- ResponseWriter wrapper prevents multiple WriteHeader calls
- Defaults to 200 OK if handler doesn't set status
- Duration measured using `time.Since()`
