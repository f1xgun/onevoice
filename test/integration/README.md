# Integration Tests

This directory contains end-to-end integration tests for the OneVoice Phase 1 Backend API.

## Overview

The integration tests verify the complete system functionality with real databases:
- PostgreSQL for relational data (users, businesses, integrations)
- MongoDB for conversations and messages
- Redis for session management and caching

## Test Coverage

### Auth Flow (`auth_test.go`)
- User registration
- User login with credentials
- Token refresh
- Get current user info (me)
- User logout
- Invalid credentials handling
- Duplicate registration prevention
- Token validation

### Business CRUD (`business_test.go`)
- Get business (before creation)
- Create business (via upsert)
- Get business details
- Update business details
- Verify persistence after updates

### Integration Management (`integration_test.go`)
- List integrations (empty state)
- Connect integration (returns 501 Not Implemented)
- Delete non-existent integration (returns 404)
- Invalid platform handling

### Conversation Management (`conversation_test.go`)
- Create conversation
- List conversations
- Get conversation by ID
- Get non-existent conversation (404)
- Invalid conversation ID (400)
- Multiple conversations

### Authorization (`authorization_test.go`)
- Multi-user isolation
- User A cannot access User B's conversations
- User B cannot access User A's conversations
- Users can only see their own resources
- Proper 403 Forbidden responses

## Running Tests

### Prerequisites

- Docker and Docker Compose
- Go 1.22+
- `migrate` CLI tool (for database migrations)

### Quick Start

Run all integration tests:

```bash
make test-integration
```

This will:
1. Start test databases in Docker containers
2. Wait for services to be healthy
3. Run database migrations
4. Execute all integration tests
5. Clean up containers and volumes

### Manual Setup

If you need to run tests manually or debug:

1. Start test environment:
```bash
cd test/integration
docker-compose -f docker-compose.test.yml up -d
```

2. Wait for services (15 seconds):
```bash
sleep 15
```

3. Run migrations:
```bash
migrate -path ./migrations/postgres \
  -database "postgres://test:test@localhost:5433/onevoice_test?sslmode=disable" up
```

4. Run tests:
```bash
TEST_API_URL=http://localhost:8081 \
TEST_POSTGRES_URL=postgres://test:test@localhost:5433/onevoice_test \
TEST_MONGO_URL=mongodb://localhost:27018 \
TEST_REDIS_URL=localhost:6380 \
go test -v ./test/integration/...
```

5. View logs (if tests fail):
```bash
cd test/integration
docker-compose -f docker-compose.test.yml logs api-test
```

6. Clean up:
```bash
cd test/integration
docker-compose -f docker-compose.test.yml down -v
```

## Test Structure

### `main_test.go`
- Test setup and teardown
- Database connections
- API health check waiting
- Database cleanup utilities

### Individual Test Files
- Each file focuses on a specific domain (auth, business, etc.)
- Tests are independent and can run in any order
- Each test suite cleans up the database before running

## Test Data

All test data is ephemeral:
- Databases run in Docker with tmpfs (RAM-only storage)
- Data is cleaned between test suites
- No persistent state between test runs

## Environment Variables

The tests use the following environment variables:

- `TEST_API_URL` - API base URL (default: http://localhost:8080)
- `TEST_POSTGRES_URL` - PostgreSQL connection string
- `TEST_MONGO_URL` - MongoDB connection string
- `TEST_REDIS_URL` - Redis connection string

## Troubleshooting

### Tests timeout waiting for API

Check if the API container is healthy:
```bash
docker-compose -f test/integration/docker-compose.test.yml ps
```

View API logs:
```bash
docker-compose -f test/integration/docker-compose.test.yml logs api-test
```

### Database connection errors

Ensure migrations ran successfully:
```bash
migrate -path ./migrations/postgres \
  -database "postgres://test:test@localhost:5433/onevoice_test?sslmode=disable" version
```

### Port conflicts

If ports 5433, 27018, 6380, or 8081 are already in use, you can:
1. Stop conflicting services
2. Modify `docker-compose.test.yml` to use different ports
3. Update test environment variables accordingly

## Adding New Tests

To add new integration tests:

1. Create a new test file in `test/integration/`
2. Import the test utilities from `main_test.go`
3. Call `cleanupDatabase(t)` at the start of your test suite
4. Use `setupTestUser()` helper to create authenticated users
5. Use `setupTestBusiness()` helper to create businesses
6. Follow the existing test patterns for consistency

Example:

```go
func TestNewFeature(t *testing.T) {
    cleanupDatabase(t)

    accessToken := setupTestUser(t, "test@example.com", "password123")

    t.Run("TestCase1", func(t *testing.T) {
        // Your test code here
    })
}
```

## CI/CD Integration

These tests are designed to run in CI/CD pipelines:

- Fast startup with health checks
- Automatic cleanup
- Clear error messages
- Exit codes indicate success/failure

Example GitHub Actions workflow:

```yaml
- name: Run Integration Tests
  run: make test-integration
```
