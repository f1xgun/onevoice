# MongoDB Repository Tests

## Prerequisites

These tests require a running MongoDB instance.

### Option 1: Local MongoDB

Install and start MongoDB locally on port 27017:

```bash
# macOS with Homebrew
brew install mongodb-community
brew services start mongodb-community

# Or using Docker
docker run -d -p 27017:27017 --name mongo-test mongo:7
```

### Option 2: Custom MongoDB URI

Set the `MONGODB_TEST_URI` environment variable:

```bash
export MONGODB_TEST_URI="mongodb://localhost:27017"
```

## Running Tests

```bash
# Run all MongoDB repository tests
cd services/api
go test -v ./internal/repository -run "TestConversation|TestMessage"

# Run only conversation tests
go test -v ./internal/repository -run TestConversationRepository

# Run only message tests
go test -v ./internal/repository -run TestMessageRepository
```

## Test Behavior

- Tests will **skip** if MongoDB is not available (instead of failing)
- Each test run creates a fresh `test_onevoice` database
- Database is automatically cleaned up after tests complete
- Tests are isolated - each test gets a clean database state via setup/teardown

## Test Coverage

### ConversationRepository

- ✓ Create with generated ID
- ✓ Create with custom ID
- ✓ Timestamp management
- ✓ GetByID (found and not found)
- ✓ ListByUserID with limit/offset
- ✓ Update with timestamp refresh
- ✓ Delete

### MessageRepository

- ✓ Create with generated ID
- ✓ Create with custom ID
- ✓ Create with attachments
- ✓ Create with tool calls
- ✓ Create with metadata
- ✓ Timestamp management
- ✓ ListByConversationID with limit/offset
- ✓ Chronological ordering
- ✓ CountByConversationID
