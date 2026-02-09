# Phase 1 Backend Setup Instructions

## Current Status

Task 1 domain models and repository interfaces have been created in `/pkg/domain/`:

- ✅ `pkg/go.mod` - Module declaration
- ✅ `pkg/domain/roles.go` - Role enum (Owner, Admin, Member)
- ✅ `pkg/domain/errors.go` - Sentinel errors for all domain entities
- ✅ `pkg/domain/models.go` - PostgreSQL models (User, Business, Integration, etc.)
- ✅ `pkg/domain/mongo_models.go` - MongoDB models (Conversation, Message, Review, Post, etc.)
- ✅ `pkg/domain/repository.go` - Repository interfaces

## Required: Install Go

**Go is not currently installed on this system.** You need to install Go 1.23 or later to continue.

### Installation Options

**Option 1: Homebrew (Recommended for macOS)**
```bash
brew install go
```

**Option 2: Download from official site**
Visit https://go.dev/dl/ and download the macOS installer.

**Option 3: asdf version manager**
```bash
asdf plugin add golang
asdf install golang latest
asdf global golang latest
```

### Verify Installation

After installing, verify Go is available:
```bash
go version
# Should output: go version go1.23.x darwin/arm64 (or similar)
```

## Next Steps: Complete Task 1

Once Go is installed, run these commands to complete Task 1:

### Step 1: Add Dependencies
```bash
cd /Users/f1xgun/onevoice/.worktrees/phase1-backend/pkg
go get github.com/google/uuid
go mod tidy
```

### Step 2: Verify Compilation
```bash
cd /Users/f1xgun/onevoice/.worktrees/phase1-backend/pkg
go build ./...
```

Expected output: No errors (successful compilation)

### Step 3: Commit Changes
```bash
cd /Users/f1xgun/onevoice/.worktrees/phase1-backend
git add pkg/
git commit -m "feat(domain): add domain models, errors, and repository interfaces

- PostgreSQL models: User, Business, Integration, Subscription
- MongoDB models: Conversation, Message, AgentTask, Review, Post
- Role enum with validation
- Sentinel errors for all domain entities
- Repository interfaces for data access

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

## What's Next After Task 1

After completing Task 1, you can proceed to:

- **Task 2:** Shared Packages (Logger & Crypto)
- **Task 3:** Database Migrations
- **Task 4:** Repository Layer Implementation

See `/Users/f1xgun/onevoice/.worktrees/phase1-backend/docs/plans/2026-02-09-phase1-backend-implementation.md` for the full implementation plan.

## Troubleshooting

### If `go get` fails with network errors
```bash
# Set Go proxy
export GOPROXY=https://proxy.golang.org,direct
```

### If compilation fails with import errors
```bash
# Clean module cache and retry
go clean -modcache
cd pkg
go mod tidy
go build ./...
```

### If git commit fails
Make sure you're in the correct directory:
```bash
cd /Users/f1xgun/onevoice/.worktrees/phase1-backend
git status  # Should show pkg/ as untracked
```
