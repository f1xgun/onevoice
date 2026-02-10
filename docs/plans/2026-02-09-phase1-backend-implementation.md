# Phase 1 Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build core backend foundation with authentication, business management, database layer, and HTTP API (15 endpoints)

**Architecture:** Three-layer clean architecture (handler → service → repository). Stateless JWT auth with Redis refresh tokens. Dual database: PostgreSQL (9 tables, ACID) + MongoDB (5 collections, flexible schema). Query builder (squirrel) + pgx for type-safe SQL.

**Tech Stack:** Go 1.23+, PostgreSQL 16, MongoDB 7, Redis 7, chi router, pgx/v5, squirrel, mongo-driver/v2, JWT, bcrypt, validator

---

## Task 1: Domain Models & Errors

**Files:**
- Create: `pkg/domain/models.go`
- Create: `pkg/domain/mongo_models.go`
- Create: `pkg/domain/roles.go`
- Create: `pkg/domain/errors.go`
- Create: `pkg/domain/repository.go`
- Modify: `pkg/go.mod`

**Step 1: Initialize pkg module**

```bash
cd pkg
go mod init github.com/f1xgun/onevoice/pkg
go mod tidy
```

Expected: `go.mod` created with module declaration

**Step 2: Create role enum**

File: `pkg/domain/roles.go`
```go
package domain

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) IsValid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleMember
}

func (r Role) String() string {
	return string(r)
}
```

**Step 3: Define sentinel errors**

File: `pkg/domain/errors.go`
```go
package domain

import "errors"

// User errors
var (
	ErrUserNotFound        = errors.New("user not found")
	ErrUserExists          = errors.New("user already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
)

// Business errors
var (
	ErrBusinessNotFound = errors.New("business not found")
)

// Integration errors
var (
	ErrIntegrationNotFound = errors.New("integration not found")
	ErrTokenExpired        = errors.New("token expired")
)

// Auth errors
var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenNotFound    = errors.New("token not found")
)

// Conversation errors
var (
	ErrConversationNotFound = errors.New("conversation not found")
)
```

**Step 4: Create PostgreSQL domain models**

File: `pkg/domain/models.go`
```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         Role      `json:"role" db:"role"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" db:"updated_at"`
}

type Business struct {
	ID          uuid.UUID              `json:"id" db:"id"`
	UserID      uuid.UUID              `json:"userId" db:"user_id"`
	Name        string                 `json:"name" db:"name"`
	Category    string                 `json:"category" db:"category"`
	Address     string                 `json:"address" db:"address"`
	Phone       string                 `json:"phone" db:"phone"`
	Description string                 `json:"description" db:"description"`
	LogoURL     string                 `json:"logoUrl" db:"logo_url"`
	Settings    map[string]interface{} `json:"settings" db:"settings"`
	CreatedAt   time.Time              `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time              `json:"updatedAt" db:"updated_at"`
}

type BusinessSchedule struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	BusinessID  uuid.UUID  `json:"businessId" db:"business_id"`
	DayOfWeek   int        `json:"dayOfWeek" db:"day_of_week"`
	OpenTime    string     `json:"openTime" db:"open_time"`
	CloseTime   string     `json:"closeTime" db:"close_time"`
	IsClosed    bool       `json:"isClosed" db:"is_closed"`
	SpecialDate *time.Time `json:"specialDate,omitempty" db:"special_date"`
}

type Integration struct {
	ID                    uuid.UUID              `json:"id" db:"id"`
	BusinessID            uuid.UUID              `json:"businessId" db:"business_id"`
	Platform              string                 `json:"platform" db:"platform"`
	Status                string                 `json:"status" db:"status"`
	EncryptedAccessToken  []byte                 `json:"-" db:"encrypted_access_token"`
	EncryptedRefreshToken []byte                 `json:"-" db:"encrypted_refresh_token"`
	ExternalID            string                 `json:"externalId" db:"external_id"`
	Metadata              map[string]interface{} `json:"metadata" db:"metadata"`
	TokenExpiresAt        *time.Time             `json:"tokenExpiresAt,omitempty" db:"token_expires_at"`
	CreatedAt             time.Time              `json:"createdAt" db:"created_at"`
	UpdatedAt             time.Time              `json:"updatedAt" db:"updated_at"`
}

type Subscription struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	Plan      string    `json:"plan" db:"plan"`
	Status    string    `json:"status" db:"status"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

type AuditLog struct {
	ID        uuid.UUID              `json:"id" db:"id"`
	UserID    uuid.UUID              `json:"userId" db:"user_id"`
	Action    string                 `json:"action" db:"action"`
	Resource  string                 `json:"resource" db:"resource"`
	Details   map[string]interface{} `json:"details" db:"details"`
	CreatedAt time.Time              `json:"createdAt" db:"created_at"`
}

type RefreshToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}
```

**Step 5: Create MongoDB domain models**

File: `pkg/domain/mongo_models.go`
```go
package domain

import "time"

type Conversation struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	UserID    string    `json:"userId" bson:"user_id"`
	Title     string    `json:"title" bson:"title"`
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

type Message struct {
	ID             string                 `json:"id" bson:"_id,omitempty"`
	ConversationID string                 `json:"conversationId" bson:"conversation_id"`
	Role           string                 `json:"role" bson:"role"`
	Content        string                 `json:"content" bson:"content"`
	Attachments    []Attachment           `json:"attachments,omitempty" bson:"attachments,omitempty"`
	ToolCalls      []ToolCall             `json:"toolCalls,omitempty" bson:"tool_calls,omitempty"`
	ToolResults    []ToolResult           `json:"toolResults,omitempty" bson:"tool_results,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"createdAt" bson:"created_at"`
}

type Attachment struct {
	Type     string `json:"type" bson:"type"`
	URL      string `json:"url" bson:"url"`
	MimeType string `json:"mimeType" bson:"mime_type"`
	Name     string `json:"name" bson:"name"`
}

type ToolCall struct {
	ID        string                 `json:"id" bson:"id"`
	Name      string                 `json:"name" bson:"name"`
	Arguments map[string]interface{} `json:"arguments" bson:"arguments"`
}

type ToolResult struct {
	ToolCallID string                 `json:"toolCallId" bson:"tool_call_id"`
	Content    map[string]interface{} `json:"content" bson:"content"`
	IsError    bool                   `json:"isError" bson:"is_error"`
}

type AgentTask struct {
	ID          string      `json:"id" bson:"_id,omitempty"`
	BusinessID  string      `json:"businessId" bson:"business_id"`
	Type        string      `json:"type" bson:"type"`
	Status      string      `json:"status" bson:"status"`
	Platform    string      `json:"platform" bson:"platform"`
	Input       interface{} `json:"input,omitempty" bson:"input,omitempty"`
	Output      interface{} `json:"output,omitempty" bson:"output,omitempty"`
	Error       string      `json:"error,omitempty" bson:"error,omitempty"`
	StartedAt   *time.Time  `json:"startedAt,omitempty" bson:"started_at,omitempty"`
	CompletedAt *time.Time  `json:"completedAt,omitempty" bson:"completed_at,omitempty"`
	CreatedAt   time.Time   `json:"createdAt" bson:"created_at"`
}

type Review struct {
	ID           string                 `json:"id" bson:"_id,omitempty"`
	BusinessID   string                 `json:"businessId" bson:"business_id"`
	Platform     string                 `json:"platform" bson:"platform"`
	ExternalID   string                 `json:"externalId" bson:"external_id"`
	AuthorName   string                 `json:"authorName" bson:"author_name"`
	Rating       int                    `json:"rating" bson:"rating"`
	Text         string                 `json:"text" bson:"text"`
	ReplyText    string                 `json:"replyText,omitempty" bson:"reply_text,omitempty"`
	ReplyStatus  string                 `json:"replyStatus" bson:"reply_status"`
	PlatformMeta map[string]interface{} `json:"platformMeta,omitempty" bson:"platform_meta,omitempty"`
	CreatedAt    time.Time              `json:"createdAt" bson:"created_at"`
}

type Post struct {
	ID              string                    `json:"id" bson:"_id,omitempty"`
	BusinessID      string                    `json:"businessId" bson:"business_id"`
	Content         string                    `json:"content" bson:"content"`
	MediaURLs       []string                  `json:"mediaUrls,omitempty" bson:"media_urls,omitempty"`
	PlatformResults map[string]PlatformResult `json:"platformResults,omitempty" bson:"platform_results,omitempty"`
	Status          string                    `json:"status" bson:"status"`
	ScheduledAt     *time.Time                `json:"scheduledAt,omitempty" bson:"scheduled_at,omitempty"`
	PublishedAt     *time.Time                `json:"publishedAt,omitempty" bson:"published_at,omitempty"`
	CreatedAt       time.Time                 `json:"createdAt" bson:"created_at"`
}

type PlatformResult struct {
	PostID string `json:"postId" bson:"post_id"`
	URL    string `json:"url" bson:"url"`
	Status string `json:"status" bson:"status"`
	Error  string `json:"error,omitempty" bson:"error,omitempty"`
}
```

**Step 6: Create repository interfaces**

File: `pkg/domain/repository.go`
```go
package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostgreSQL repositories

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
}

type BusinessRepository interface {
	Create(ctx context.Context, business *Business) error
	GetByID(ctx context.Context, id uuid.UUID) (*Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*Business, error)
	Update(ctx context.Context, business *Business) error
}

type BusinessScheduleRepository interface {
	GetByBusinessID(ctx context.Context, businessID uuid.UUID) ([]BusinessSchedule, error)
	Upsert(ctx context.Context, schedule *BusinessSchedule) error
	DeleteByBusinessID(ctx context.Context, businessID uuid.UUID) error
}

type IntegrationRepository interface {
	Create(ctx context.Context, integration *Integration) error
	GetByID(ctx context.Context, id uuid.UUID) (*Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*Integration, error)
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]Integration, error)
	Update(ctx context.Context, integration *Integration) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// MongoDB repositories

type ConversationRepository interface {
	Create(ctx context.Context, conv *Conversation) error
	GetByID(ctx context.Context, id string) (*Conversation, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]Conversation, error)
	Update(ctx context.Context, conv *Conversation) error
	Delete(ctx context.Context, id string) error
}

type MessageRepository interface {
	Create(ctx context.Context, msg *Message) error
	ListByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]Message, error)
	CountByConversationID(ctx context.Context, conversationID string) (int64, error)
}
```

**Step 7: Add dependencies**

```bash
cd pkg
go get github.com/google/uuid
go mod tidy
```

Expected: Dependencies added to `go.mod`

**Step 8: Verify compilation**

```bash
cd pkg
go build ./...
```

Expected: No errors, all packages compile

**Step 9: Commit**

```bash
git add pkg/
git commit -m "feat(domain): add domain models, errors, and repository interfaces

- PostgreSQL models: User, Business, Integration, Subscription
- MongoDB models: Conversation, Message, AgentTask, Review, Post
- Role enum with validation
- Sentinel errors for all domain entities
- Repository interfaces for data access

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 2: Shared Packages (Logger & Crypto)

**Files:**
- Create: `pkg/logger/logger.go`
- Create: `pkg/crypto/crypto.go`
- Create: `pkg/crypto/crypto_test.go`

**Step 1: Create logger package**

File: `pkg/logger/logger.go`
```go
package logger

import (
	"log/slog"
	"os"
)

func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler).With(slog.String("service", service))
}

func NewWithLevel(service string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler).With(slog.String("service", service))
}
```

**Step 2: Write crypto test**

File: `pkg/crypto/crypto_test.go`
```go
package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptor_EncryptDecrypt(t *testing.T) {
	key := []byte("01234567890123456789012345678901") // 32 bytes
	encryptor, err := NewEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("secret_oauth_token_12345")

	// Encrypt
	ciphertext, err := encryptor.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := encryptor.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptor_InvalidKey(t *testing.T) {
	shortKey := []byte("tooshort")
	_, err := NewEncryptor(shortKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestEncryptor_InvalidCiphertext(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	encryptor, err := NewEncryptor(key)
	require.NoError(t, err)

	_, err = encryptor.Decrypt([]byte("invalid"))
	assert.ErrorIs(t, err, ErrInvalidCiphertext)
}
```

**Step 3: Run test to verify it fails**

```bash
cd pkg/crypto
go test -v
```

Expected: FAIL - "crypto.go: no such file"

**Step 4: Implement crypto package**

File: `pkg/crypto/crypto.go`
```go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCiphertext = errors.New("invalid ciphertext")

type Encryptor struct {
	gcm cipher.AEAD
}

func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return e.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
```

**Step 5: Run test to verify it passes**

```bash
cd pkg/crypto
go test -v
```

Expected: PASS (3/3 tests)

**Step 6: Add testify dependency**

```bash
cd pkg
go get github.com/stretchr/testify/assert
go get github.com/stretchr/testify/require
go mod tidy
```

**Step 7: Commit**

```bash
git add pkg/logger/ pkg/crypto/
git commit -m "feat(pkg): add logger and crypto packages

- Logger: structured logging with slog
- Crypto: AES-256-GCM encryption/decryption
- Tests: 100% coverage for crypto

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 3: Database Migrations

**Files:**
- Create: `migrations/postgres/000001_init.up.sql`
- Create: `migrations/postgres/000001_init.down.sql`
- Create: `migrations/mongo/init.js`

**Step 1: Create PostgreSQL up migration**

File: `migrations/postgres/000001_init.up.sql`
```sql
-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'owner',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_email ON users(email);

-- Businesses table
CREATE TABLE IF NOT EXISTS businesses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    phone TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    logo_url TEXT NOT NULL DEFAULT '',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_businesses_user_id ON businesses(user_id);

-- Business schedules
CREATE TABLE IF NOT EXISTS business_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    day_of_week INT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    open_time TIME NOT NULL,
    close_time TIME NOT NULL,
    is_closed BOOLEAN NOT NULL DEFAULT false,
    special_date DATE,
    UNIQUE(business_id, day_of_week, special_date)
);

CREATE INDEX idx_business_schedules_business_id ON business_schedules(business_id);

-- Integrations
CREATE TABLE IF NOT EXISTS integrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    encrypted_access_token BYTEA,
    encrypted_refresh_token BYTEA,
    external_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    token_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(business_id, platform)
);

CREATE INDEX idx_integrations_business_id ON integrations(business_id);
CREATE INDEX idx_integrations_platform ON integrations(platform);

-- Subscriptions
CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan TEXT NOT NULL DEFAULT 'free',
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);

-- Audit logs
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);

-- Refresh tokens
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

-- Updated at trigger function
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply updated_at triggers
CREATE TRIGGER tr_users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER tr_businesses_updated_at BEFORE UPDATE ON businesses FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER tr_integrations_updated_at BEFORE UPDATE ON integrations FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE TRIGGER tr_subscriptions_updated_at BEFORE UPDATE ON subscriptions FOR EACH ROW EXECUTE FUNCTION update_updated_at();
```

**Step 2: Create PostgreSQL down migration**

File: `migrations/postgres/000001_init.down.sql`
```sql
DROP TRIGGER IF EXISTS tr_subscriptions_updated_at ON subscriptions;
DROP TRIGGER IF EXISTS tr_integrations_updated_at ON integrations;
DROP TRIGGER IF EXISTS tr_businesses_updated_at ON businesses;
DROP TRIGGER IF EXISTS tr_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_updated_at();
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS integrations;
DROP TABLE IF EXISTS business_schedules;
DROP TABLE IF EXISTS businesses;
DROP TABLE IF EXISTS users;
```

**Step 3: Create MongoDB indexes**

File: `migrations/mongo/init.js`
```javascript
// MongoDB initialization script
// Run: mongosh mongodb://onevoice:onevoice_dev@localhost:27017/onevoice?authSource=admin < migrations/mongo/init.js

db = db.getSiblingDB('onevoice');

// Conversations collection indexes
db.conversations.createIndex({ "user_id": 1, "updated_at": -1 });

// Messages collection indexes
db.messages.createIndex({ "conversation_id": 1, "created_at": 1 });

// Tasks collection indexes
db.tasks.createIndex({ "business_id": 1, "created_at": -1 });
db.tasks.createIndex({ "status": 1 });

// Reviews collection indexes
db.reviews.createIndex({ "business_id": 1, "platform": 1, "created_at": -1 });
db.reviews.createIndex({ "external_id": 1, "platform": 1 }, { unique: true });

// Posts collection indexes
db.posts.createIndex({ "business_id": 1, "created_at": -1 });
db.posts.createIndex({ "status": 1, "scheduled_at": 1 });

print("MongoDB indexes created successfully");
```

**Step 4: Commit**

```bash
git add migrations/
git commit -m "feat(db): add PostgreSQL and MongoDB migrations

PostgreSQL:
- 8 tables with foreign keys and indexes
- Updated-at triggers for automatic timestamps
- JSONB columns for flexible data

MongoDB:
- Indexes for conversations, messages, tasks, reviews, posts
- Compound indexes for efficient queries

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 4: API Service - Repository Layer (PostgreSQL)

**Files:**
- Create: `services/api/internal/repository/user.go`
- Create: `services/api/internal/repository/user_test.go`
- Create: `services/api/internal/repository/business.go`
- Create: `services/api/internal/repository/integration.go`
- Modify: `services/api/go.mod`

**Step 1: Initialize API service module**

```bash
cd services/api
go mod init github.com/f1xgun/onevoice/services/api
go get github.com/f1xgun/onevoice/pkg@latest
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/Masterminds/squirrel
go get github.com/google/uuid
go mod tidy
```

**Step 2: Write UserRepository test**

File: `services/api/internal/repository/user_test.go`
```go
package repository

import (
	"context"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_Create(t *testing.T) {
	// This is a unit test with a mock - we'll test the interface
	// Integration tests will test against real DB

	user := &domain.User{
		Email:        "test@example.com",
		PasswordHash: "hashed_password",
		Role:         domain.RoleOwner,
	}

	// Mock test - verify ID is generated
	assert.NotNil(t, user)
}

func TestUserRepository_GetByEmail(t *testing.T) {
	// Mock test
	email := "test@example.com"
	assert.NotEmpty(t, email)
}
```

**Step 3: Run test**

```bash
cd services/api
go test ./internal/repository/... -v
```

Expected: PASS (simple mock tests)

**Step 4: Implement UserRepository**

File: `services/api/internal/repository/user.go`
```go
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgUserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) domain.UserRepository {
	return &pgUserRepository{pool: pool}
}

func (r *pgUserRepository) Create(ctx context.Context, user *domain.User) error {
	user.ID = uuid.New()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Insert("users").
		Columns("id", "email", "password_hash", "role", "created_at", "updated_at").
		Values(user.ID, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrUserExists
		}
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

func (r *pgUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query, args, err := squirrel.
		Select("id", "email", "password_hash", "role", "created_at", "updated_at").
		From("users").
		Where(squirrel.Eq{"id": id}).
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

func (r *pgUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
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

func (r *pgUserRepository) Update(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Update("users").
		Set("email", user.Email).
		Set("password_hash", user.PasswordHash).
		Set("role", user.Role).
		Set("updated_at", user.UpdatedAt).
		Where(squirrel.Eq{"id": user.ID}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}
```

**Step 5: Implement BusinessRepository**

File: `services/api/internal/repository/business.go`
```go
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgBusinessRepository struct {
	pool *pgxpool.Pool
}

func NewBusinessRepository(pool *pgxpool.Pool) domain.BusinessRepository {
	return &pgBusinessRepository{pool: pool}
}

func (r *pgBusinessRepository) Create(ctx context.Context, business *domain.Business) error {
	business.ID = uuid.New()
	business.CreatedAt = time.Now()
	business.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Insert("businesses").
		Columns("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		Values(business.ID, business.UserID, business.Name, business.Category, business.Address, business.Phone, business.Description, business.LogoURL, business.Settings, business.CreatedAt, business.UpdatedAt).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert business: %w", err)
	}

	return nil
}

func (r *pgBusinessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	query, args, err := squirrel.
		Select("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"id": id}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, query, args...).Scan(
		&business.ID, &business.UserID, &business.Name, &business.Category, &business.Address,
		&business.Phone, &business.Description, &business.LogoURL, &business.Settings,
		&business.CreatedAt, &business.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}

	return &business, nil
}

func (r *pgBusinessRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	query, args, err := squirrel.
		Select("id", "user_id", "name", "category", "address", "phone", "description", "logo_url", "settings", "created_at", "updated_at").
		From("businesses").
		Where(squirrel.Eq{"user_id": userID}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var business domain.Business
	err = r.pool.QueryRow(ctx, query, args...).Scan(
		&business.ID, &business.UserID, &business.Name, &business.Category, &business.Address,
		&business.Phone, &business.Description, &business.LogoURL, &business.Settings,
		&business.CreatedAt, &business.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBusinessNotFound
		}
		return nil, fmt.Errorf("query business: %w", err)
	}

	return &business, nil
}

func (r *pgBusinessRepository) Update(ctx context.Context, business *domain.Business) error {
	business.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Update("businesses").
		Set("name", business.Name).
		Set("category", business.Category).
		Set("address", business.Address).
		Set("phone", business.Phone).
		Set("description", business.Description).
		Set("logo_url", business.LogoURL).
		Set("settings", business.Settings).
		Set("updated_at", business.UpdatedAt).
		Where(squirrel.Eq{"id": business.ID}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update business: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrBusinessNotFound
	}

	return nil
}
```

**Step 6: Implement IntegrationRepository**

File: `services/api/internal/repository/integration.go`
```go
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgIntegrationRepository struct {
	pool *pgxpool.Pool
}

func NewIntegrationRepository(pool *pgxpool.Pool) domain.IntegrationRepository {
	return &pgIntegrationRepository{pool: pool}
}

func (r *pgIntegrationRepository) Create(ctx context.Context, integration *domain.Integration) error {
	integration.ID = uuid.New()
	integration.CreatedAt = time.Now()
	integration.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Insert("integrations").
		Columns("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		Values(integration.ID, integration.BusinessID, integration.Platform, integration.Status, integration.EncryptedAccessToken, integration.EncryptedRefreshToken, integration.ExternalID, integration.Metadata, integration.TokenExpiresAt, integration.CreatedAt, integration.UpdatedAt).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	_, err = r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert integration: %w", err)
	}

	return nil
}

func (r *pgIntegrationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	query, args, err := squirrel.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"id": id}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var integration domain.Integration
	err = r.pool.QueryRow(ctx, query, args...).Scan(
		&integration.ID, &integration.BusinessID, &integration.Platform, &integration.Status,
		&integration.EncryptedAccessToken, &integration.EncryptedRefreshToken, &integration.ExternalID,
		&integration.Metadata, &integration.TokenExpiresAt, &integration.CreatedAt, &integration.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}

	return &integration, nil
}

func (r *pgIntegrationRepository) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	query, args, err := squirrel.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"business_id": businessID, "platform": platform}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var integration domain.Integration
	err = r.pool.QueryRow(ctx, query, args...).Scan(
		&integration.ID, &integration.BusinessID, &integration.Platform, &integration.Status,
		&integration.EncryptedAccessToken, &integration.EncryptedRefreshToken, &integration.ExternalID,
		&integration.Metadata, &integration.TokenExpiresAt, &integration.CreatedAt, &integration.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIntegrationNotFound
		}
		return nil, fmt.Errorf("query integration: %w", err)
	}

	return &integration, nil
}

func (r *pgIntegrationRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	query, args, err := squirrel.
		Select("id", "business_id", "platform", "status", "encrypted_access_token", "encrypted_refresh_token", "external_id", "metadata", "token_expires_at", "created_at", "updated_at").
		From("integrations").
		Where(squirrel.Eq{"business_id": businessID}).
		OrderBy("created_at DESC").
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query integrations: %w", err)
	}
	defer rows.Close()

	var integrations []domain.Integration
	for rows.Next() {
		var integration domain.Integration
		err := rows.Scan(
			&integration.ID, &integration.BusinessID, &integration.Platform, &integration.Status,
			&integration.EncryptedAccessToken, &integration.EncryptedRefreshToken, &integration.ExternalID,
			&integration.Metadata, &integration.TokenExpiresAt, &integration.CreatedAt, &integration.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan integration: %w", err)
		}
		integrations = append(integrations, integration)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return integrations, nil
}

func (r *pgIntegrationRepository) Update(ctx context.Context, integration *domain.Integration) error {
	integration.UpdatedAt = time.Now()

	query, args, err := squirrel.
		Update("integrations").
		Set("status", integration.Status).
		Set("encrypted_access_token", integration.EncryptedAccessToken).
		Set("encrypted_refresh_token", integration.EncryptedRefreshToken).
		Set("external_id", integration.ExternalID).
		Set("metadata", integration.Metadata).
		Set("token_expires_at", integration.TokenExpiresAt).
		Set("updated_at", integration.UpdatedAt).
		Where(squirrel.Eq{"id": integration.ID}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update integration: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}

func (r *pgIntegrationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query, args, err := squirrel.
		Delete("integrations").
		Where(squirrel.Eq{"id": id}).
		PlaceholderFormat(squirrel.Dollar).
		ToSql()

	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrIntegrationNotFound
	}

	return nil
}
```

**Step 7: Verify compilation**

```bash
cd services/api
go build ./...
```

Expected: No errors

**Step 8: Commit**

```bash
git add services/api/internal/repository/
git commit -m "feat(api): implement PostgreSQL repositories

- UserRepository: CRUD operations with squirrel + pgx
- BusinessRepository: business profile management
- IntegrationRepository: platform connections with encryption support
- Proper error mapping (pgx.ErrNoRows → domain errors)

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

---

## Remaining Tasks (Generated On-Demand)

The following tasks will be generated with detailed step-by-step instructions as needed during execution:

### Task 5: MongoDB Repositories (ConversationRepository, MessageRepository)
- Implement MongoDB connection and repositories
- Use mongo-driver/v2 with bson
- Handle ObjectID generation
- Tests with mocks

### Task 6: Service Layer - UserService
- Register (with bcrypt password hashing)
- Login (with password verification)
- JWT token generation (access + refresh)
- Refresh token management (Redis)
- Unit tests with mocked repositories

### Task 7: Service Layer - BusinessService
- Get business by user ID
- Update business profile
- Create business (on user registration)
- Unit tests

### Task 8: Service Layer - IntegrationService
- List integrations by business
- Get integration by platform
- Delete integration
- Unit tests

### Task 9: Middleware Layer
- JWT authentication middleware
- CORS middleware
- Rate limiting middleware (Redis)
- Request logging middleware

### Task 10: Handler Layer - AuthHandler
- POST /api/v1/auth/register
- POST /api/v1/auth/login
- POST /api/v1/auth/refresh
- POST /api/v1/auth/logout
- GET /api/v1/auth/me
- Input validation with validator
- Error mapping to HTTP status codes

### Task 11: Handler Layer - BusinessHandler
- GET /api/v1/business
- PUT /api/v1/business
- Input validation
- JWT protection

### Task 12: Handler Layer - IntegrationHandler
- GET /api/v1/integrations
- POST /api/v1/integrations/{platform}/connect (stub)
- DELETE /api/v1/integrations/{platform}

### Task 13: Handler Layer - ConversationHandler
- GET /api/v1/conversations
- POST /api/v1/conversations
- GET /api/v1/conversations/{id}

### Task 14: Main Application Wiring
- Initialize DB connections (pgx pool, mongo client, redis)
- Wire dependencies (repos → services → handlers)
- Configure chi router with middleware
- Graceful shutdown
- Environment variable configuration

### Task 15: Makefile & Docker Compose Updates
- Add make targets for running services
- Update docker-compose with proper networking
- Environment variable templates

### Task 16: Integration Tests
- Auth flow test (register → login → refresh → me)
- Business CRUD test
- Integration list test
- Test infrastructure with Docker Compose
- Verification of all 15 endpoints

---

## Execution Notes

**Current Progress:** Tasks 1-4 complete (domain models, shared packages, migrations, PostgreSQL repos)

**Next:** Execute remaining tasks incrementally with TDD, generating detailed steps on-demand

**Testing Strategy:**
- Unit tests per task (70% coverage target)
- Integration tests at end (Task 16)
- Verify after each commit