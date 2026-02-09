# Phase 1 Backend Design

**Date:** 2026-02-09
**Status:** Approved
**Estimated Duration:** 8-9 days

## Overview

Phase 1 implements the core backend foundation for OneVoice: authentication, business management, database layer, and HTTP API. This phase establishes the architectural patterns that all subsequent phases will follow.

## Design Decisions

### 1. Architecture & Layer Responsibilities

**Three-layer architecture:**

```
HTTP Handler Layer (services/api/internal/handler/)
    ↓ validates input, maps to/from JSON
Service Layer (services/api/internal/service/)
    ↓ business logic, orchestrates repositories
Repository Layer (services/api/internal/repository/)
    ↓ data access via squirrel + pgx
Database (PostgreSQL + MongoDB)
```

**Layer responsibilities:**

**Handlers** (`handler/auth.go`, `handler/business.go`, etc.)
- Parse HTTP requests, validate with `validator` library
- Call service methods
- Map errors to HTTP status codes (404, 400, 401, 403, 500)
- Return JSON responses (direct format, no envelope)
- Never contain business logic

**Services** (`service/user.go`, `service/business.go`, etc.)
- Implement business logic (e.g., check if email exists before creating user)
- Orchestrate multiple repository calls if needed
- Hash passwords, generate JWT tokens
- Return domain errors (e.g., `domain.ErrUserNotFound`)
- Can call other services

**Repositories** (`repository/user.go`, `repository/business.go`, etc.)
- SQL queries via squirrel query builder + pgx
- Map database rows to domain models
- Return `pgx.ErrNoRows` as domain errors (e.g., `domain.ErrUserNotFound`)
- Transaction support for multi-table operations

### 2. Authentication System (JWT + Redis)

**Approach:** Stateless JWT with Redis-backed refresh tokens

**Access Token** (15 min expiry, stateless)
```go
Claims {
    UserID    uuid.UUID
    Email     string
    Role      Role      // domain.RoleOwner | RoleAdmin | RoleMember
    ExpiresAt time.Time
    IssuedAt  time.Time
}
```
- Signed with HMAC-SHA256 (`JWT_SECRET` env var)
- Included in `Authorization: Bearer <token>` header
- No database lookup needed to validate

**Refresh Token** (7 day expiry, stored in Redis)
```go
RefreshToken {
    ID        uuid.UUID  // random token ID
    UserID    uuid.UUID
    TokenHash string     // SHA-256 hash of token
    ExpiresAt time.Time
}
```
- Stored in Redis: `refresh_token:{tokenID} -> userID`
- TTL set to 7 days (auto-cleanup)
- On use: rotate to new token (invalidate old one)
- Allows logout by deleting from Redis

**Role constants** (`pkg/domain/roles.go`):
```go
type Role string

const (
    RoleOwner  Role = "owner"   // Business owner, full access
    RoleAdmin  Role = "admin"   // Can manage integrations, posts
    RoleMember Role = "member"  // Read-only access
)

func (r Role) IsValid() bool {
    return r == RoleOwner || r == RoleAdmin || r == RoleMember
}
```

**Auth endpoints:**
- `POST /api/v1/auth/register` → create user, return access + refresh tokens
- `POST /api/v1/auth/login` → verify password, return access + refresh tokens
- `POST /api/v1/auth/refresh` → validate refresh token, return new pair
- `POST /api/v1/auth/logout` → delete refresh token from Redis
- `GET /api/v1/auth/me` → get current user profile

**Middleware:**
- `JWTAuth` middleware validates access token on protected routes
- Extracts `UserID` and `Role` from claims, adds to request context
- Returns 401 if token missing/invalid/expired

### 3. Database Schema

**Tool choice:** Query builder (squirrel) + pgx for PostgreSQL, mongo-driver for MongoDB

**Validation strategy:** Handler layer only (format validation), service layer handles business rules

**Error handling:** Sentinel errors + wrapping (e.g., `domain.ErrUserNotFound`)

**API response format:** Direct response with HTTP status codes (no envelope)

**Repository pattern:** Fine-grained methods (GetByID, GetByEmail, Create, Update, Delete, List)

#### PostgreSQL (9 tables - relational data)

1. **users** - Authentication & roles
   - Columns: id (UUID PK), email (unique), password_hash, role, created_at, updated_at
   - Indexes: email

2. **businesses** - Business profiles
   - Columns: id (UUID PK), user_id (FK), name, category, address, phone, description, logo_url, settings (JSONB), created_at, updated_at
   - Indexes: user_id
   - 1-to-1 with users (each user has one business for MVP)

3. **business_schedules** - Operating hours
   - Columns: id (UUID PK), business_id (FK), day_of_week (0-6), open_time, close_time, is_closed, special_date (nullable)
   - Unique: (business_id, day_of_week, special_date)
   - Supports regular hours + special dates (holidays)

4. **integrations** - Platform connections
   - Columns: id (UUID PK), business_id (FK), platform (google|vk|telegram), status (pending|active|error), encrypted_access_token (bytea), encrypted_refresh_token (bytea), external_id, metadata (JSONB), token_expires_at, created_at, updated_at
   - Unique: (business_id, platform)
   - Tokens encrypted with AES-256-GCM before storage

5. **subscriptions** - Billing plans
   - Columns: id, user_id (FK), plan (free|pro|enterprise), status (active|expired), expires_at, created_at, updated_at

6. **audit_logs** - Security audit trail
   - Columns: id, user_id (FK), action, resource, details (JSONB), created_at
   - Never deleted, append-only

7. **refresh_tokens** - JWT refresh tokens (backup to Redis)
   - Columns: id, user_id (FK), token_hash, expires_at, created_at
   - Note: Primary storage is Redis; this is backup for recovery

8. **platform_sync_status** - Sync conflict detection
   - Columns: id, business_id, platform, field (phone|address|hours), local_value, remote_value, status (synced|conflict), checked_at

9. **Updated at triggers** - Automatic timestamp updates

#### MongoDB (5 collections - flexible schema)

**Why MongoDB for these entities:**
- Varied schemas per platform (different review/post structures)
- Nested documents (messages with tool calls, attachments)
- High write volume (chat messages, task logs)
- No complex joins needed

1. **conversations** - Chat sessions
   - Fields: _id, user_id, title, created_at, updated_at
   - Index: {user_id: 1, updated_at: -1}

2. **messages** - Chat messages with LLM interactions
   - Fields: _id, conversation_id, role, content, tool_calls[], tool_results[], attachments[], metadata, created_at
   - Index: {conversation_id: 1, created_at: 1}

3. **tasks** - Agent execution logs
   - Fields: _id, business_id, type, status, platform, input, output, error, started_at, completed_at, created_at
   - Indexes: {business_id: 1, created_at: -1}, {status: 1}

4. **reviews** - Platform reviews (varied schemas)
   - Fields: _id, business_id, platform, external_id, author_name, rating, text, reply_text, reply_status, platform_meta, created_at
   - Index: {business_id: 1, platform: 1, created_at: -1}
   - Unique: {platform: 1, external_id: 1}

5. **posts** - Published content across platforms
   - Fields: _id, business_id, content, media_urls[], platform_results{}, status, scheduled_at, published_at, created_at
   - Indexes: {business_id: 1, created_at: -1}, {status: 1, scheduled_at: 1}

**Total: 9 PostgreSQL tables + 5 MongoDB collections = 14 collections** (exceeds thesis requirement of 10)

### 4. Repository Implementations

**PostgreSQL repositories** (using squirrel + pgx):

Key patterns:
- All methods take `context.Context` for cancellation
- Convert `pgx.ErrNoRows` → domain errors (e.g., `domain.ErrUserNotFound`)
- Wrap all errors with `fmt.Errorf(..., %w, err)` for traceability
- Generate IDs in repository (UUID for PG)
- Set timestamps in repository
- Use squirrel for type-safe query building

Example methods per repository:
- `Create(ctx, entity) error`
- `GetByID(ctx, id) (*Entity, error)`
- `Update(ctx, entity) error`
- `Delete(ctx, id) error`
- `List(ctx, limit, offset) ([]Entity, error)`
- Custom queries: `GetByEmail`, `GetByUserID`, etc.

**MongoDB repositories** (using mongo-driver):

Key patterns:
- Generate ObjectID for _id field
- Use bson.M for filters
- Set sort, limit, skip with options
- Handle cursor iteration
- Convert Mongo errors to domain errors

### 5. Service Layer (Business Logic)

**Service responsibilities:**
- Orchestrate repository calls
- Implement business rules (check duplicates, validate state)
- Hash passwords with bcrypt
- Generate JWT tokens
- Log important actions (with user/business IDs, never secrets)
- Return domain errors with context

**Key services:**
- **UserService** - Register, Login, GetProfile, ChangePassword
- **BusinessService** - Get, Update, UpdateSchedule
- **IntegrationService** - List, Connect (OAuth), Disconnect, GetToken (internal)
- **ConversationService** - Create, List, GetWithMessages

**Service patterns:**
- Services can depend on multiple repositories
- Services can call other services
- Always wrap errors with context
- Use structured logging (slog)

### 6. HTTP Handler Layer & API Endpoints

**Handler responsibilities:**
- Parse & validate requests with `validator` library
- Call service methods
- Map errors to HTTP status codes
- Return JSON responses (direct format)

**Phase 1 API Endpoints (15 total):**

**Auth (5 endpoints)**
```
POST   /api/v1/auth/register      → 201 + {user, accessToken, refreshToken}
POST   /api/v1/auth/login         → 200 + {user, accessToken, refreshToken}
POST   /api/v1/auth/refresh       → 200 + {accessToken, refreshToken}
POST   /api/v1/auth/logout        → 204
GET    /api/v1/auth/me            → 200 + {user}
```

**Business (4 endpoints)**
```
GET    /api/v1/business           → 200 + {business}
PUT    /api/v1/business           → 200 + {business}
GET    /api/v1/business/schedule  → 200 + {schedules[]}
PUT    /api/v1/business/schedule  → 200 + {schedules[]}
```

**Integrations (3 endpoints)**
```
GET    /api/v1/integrations                    → 200 + {integrations[]}
POST   /api/v1/integrations/{platform}/connect → 302 redirect to OAuth
DELETE /api/v1/integrations/{platform}         → 204
```

**Conversations (3 endpoints)**
```
GET    /api/v1/conversations      → 200 + {conversations[]}
POST   /api/v1/conversations      → 201 + {conversation}
GET    /api/v1/conversations/{id} → 200 + {conversation, messages[]}
```

**Middleware stack:**
- `middleware.RequestID` - Add request ID to context
- `middleware.Logger` - Log all requests (slog)
- `middleware.Recoverer` - Panic recovery
- `corsMiddleware` - CORS headers
- `rateLimitMiddleware` - Rate limiting (Redis token bucket)
- `jwtAuthMiddleware` - JWT validation (protected routes only)

**Error mapping:**
```
domain.ErrUserNotFound        → 404
domain.ErrBusinessNotFound    → 404
domain.ErrUserExists          → 409
domain.ErrInvalidCredentials  → 401
domain.ErrUnauthorized        → 401
domain.ErrForbidden           → 403
validation errors             → 400
unknown errors                → 500 (log, don't expose details)
```

### 7. Testing Strategy

**Approach:** Unit tests (~70%) + Integration tests (~30%)

**Unit tests:**
- Service layer: Mock repositories, test business logic
- Repository layer: In-memory mock or testcontainers
- Use `testify/assert` for assertions
- Table-driven tests for multiple scenarios

**Integration tests:**
- `_integration_test.go` files with `// +build integration` tag
- Docker Compose test environment (PostgreSQL, MongoDB, Redis)
- Test full request flow: HTTP → handler → service → repository → DB
- Verify JWT flows, CRUD operations, error handling

**Test infrastructure:**
- `make test` - Run unit tests
- `make test-integration` - Run integration tests (requires Docker)
- CI runs both in parallel
- Use `testcontainers-go` for real database instances

**Coverage targets:**
- Service layer: 80%+ (critical business logic)
- Repository layer: 70%+
- Handler layer: 60%+ (mostly glue code)
- Overall: 70%+

## Implementation Steps

1. **Domain models** (`pkg/domain/`)
   - models.go (PostgreSQL entities)
   - mongo_models.go (MongoDB entities)
   - repository.go (interfaces)
   - roles.go (Role enum)
   - errors.go (sentinel errors)

2. **Database setup**
   - PostgreSQL migrations (migrations/postgres/)
   - MongoDB indexes (migrations/mongo/init.js)
   - Run `make migrate` to apply

3. **Repository layer** (`services/api/internal/repository/`)
   - PostgreSQL repos: user.go, business.go, integration.go
   - MongoDB repos: conversation.go, message.go
   - Unit tests with mocks

4. **Service layer** (`services/api/internal/service/`)
   - user.go (Register, Login, Refresh, Logout)
   - business.go (Get, Update, UpdateSchedule)
   - integration.go (List, Connect, Disconnect)
   - conversation.go (Create, List, GetWithMessages)
   - Unit tests with mocked repositories

5. **Middleware** (`services/api/internal/middleware/`)
   - jwt.go (JWTAuth middleware)
   - cors.go (CORS)
   - ratelimit.go (Redis-based rate limiting)
   - logger.go (Request logging)

6. **Handler layer** (`services/api/internal/handler/`)
   - auth.go (5 endpoints)
   - business.go (4 endpoints)
   - integration.go (3 endpoints)
   - conversation.go (3 endpoints)
   - Integration tests

7. **Main application** (`services/api/cmd/main.go`)
   - Initialize DB connections (pgx pool, mongo client, redis)
   - Wire up dependencies (repos → services → handlers)
   - Configure router with middleware
   - Graceful shutdown

## Success Criteria

- [ ] All 15 API endpoints functional
- [ ] JWT authentication flow works (register → login → refresh → logout)
- [ ] Business CRUD operations work
- [ ] Integration list endpoint works (OAuth flows in Phase 6)
- [ ] Conversation CRUD works
- [ ] Unit test coverage ≥70%
- [ ] Integration tests pass
- [ ] All endpoints documented
- [ ] Can register user, create business, view profile via API

## Dependencies

**Go packages:**
- `github.com/jackc/pgx/v5` - PostgreSQL
- `github.com/Masterminds/squirrel` - Query builder
- `go.mongodb.org/mongo-driver/v2` - MongoDB
- `github.com/redis/go-redis/v9` - Redis
- `github.com/go-chi/chi/v5` - Router
- `github.com/golang-jwt/jwt/v5` - JWT
- `github.com/go-playground/validator/v10` - Validation
- `github.com/google/uuid` - UUIDs
- `golang.org/x/crypto/bcrypt` - Password hashing
- `github.com/stretchr/testify` - Testing

**Infrastructure:**
- Docker Compose with PostgreSQL 16, MongoDB 7, Redis 7
- `golang-migrate` for SQL migrations

## Timeline

- Days 1-2: Domain models, database schema, migrations
- Days 3-4: Repository layer + unit tests
- Days 5-6: Service layer + unit tests
- Days 7-8: Handler layer, middleware, integration tests
- Day 9: Documentation, final testing, cleanup

## Next Phase

After Phase 1 completion:
- **Phase 2:** LLM Orchestrator (OpenAI/Anthropic client, agent loop, NATS, SSE streaming)
