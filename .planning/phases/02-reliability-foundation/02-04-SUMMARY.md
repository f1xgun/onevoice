# PLAN-2.4 Summary: Replace panic() in production handlers with error returns

## Status: COMPLETE

## What was done

Replaced all 10 `panic()` calls across 8 constructors in the API service with proper `(Type, error)` return signatures. Updated `services/api/cmd/main.go` to handle constructor errors via the existing `run() error` flow. Malformed startup configuration now results in a clean log + exit(1) instead of a stack trace crash.

### Files modified
- `services/api/internal/handler/auth.go` — `NewAuthHandler` returns `(*AuthHandler, error)`
- `services/api/internal/handler/business.go` — `NewBusinessHandler` returns `(*BusinessHandler, error)`
- `services/api/internal/handler/integration.go` — `NewIntegrationHandler` returns `(*IntegrationHandler, error)`
- `services/api/internal/handler/conversation.go` — `NewConversationHandler` returns `(*ConversationHandler, error)`
- `services/api/internal/handler/review.go` — `NewReviewHandler` returns `(*ReviewHandler, error)`
- `services/api/internal/handler/post.go` — `NewPostHandler` returns `(*PostHandler, error)`
- `services/api/internal/handler/agent_task.go` — `NewAgentTaskHandler` returns `(*AgentTaskHandler, error)`
- `services/api/internal/service/user.go` — `NewUserService` returns `(UserService, error)`
- `services/api/cmd/main.go` — all constructor calls updated with `if err != nil { return fmt.Errorf(...) }`

### Test files added/updated
- `services/api/internal/handler/constructor_test.go` — 9 new tests for nil-dependency error paths
- `services/api/internal/service/user_constructor_test.go` — 2 new tests (short secret error, valid secret success)
- `services/api/internal/handler/integration_test.go` — updated all `NewIntegrationHandler` calls for new signature
- `services/api/internal/handler/auth_test.go` — updated for new signature
- `services/api/internal/handler/business_test.go` — updated for new signature
- `services/api/internal/handler/conversation_test.go` — updated for new signature
- `services/api/internal/service/user_test.go` — updated for new signature

## Verification
- Zero `panic()` calls in production handler/service code
- All 8 constructors return `(Type, error)`
- 11 constructor tests pass (9 handler + 2 service)
- `GOWORK=off go build ./...` passes
- `GOWORK=off go test -race ./...` passes
