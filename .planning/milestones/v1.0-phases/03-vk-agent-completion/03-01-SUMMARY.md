# SUMMARY 03-01: VK Photo Post Tool — Two-Step Upload Flow

## Status: Complete

## What was done

### Task 1: PostPhoto method on VKClient interface and implementation
- Added `PostPhoto(groupID string, photoURL, caption string) (int64, error)` to the `VKClient` interface in `handler.go`
- Implemented in `client.go`: downloads image via `http.Client` with 30s timeout, validates status 200 and `image/*` Content-Type, calls `UploadGroupWallPhoto`, formats `photo{ownerID}_{id}` attachment, publishes via `WallPost`

### Task 2: vk__post_photo handler case and input validation
- Added `case "vk__post_photo"` to the Handle switch
- Added `postPhoto` handler method with `photo_url` required validation (returns `NonRetryableError` on missing)

### Task 3: Unit tests for vk__post_photo handler
- `TestHandler_PostPhoto` — success: verifies groupID, photoURL, caption passed through, returns post_id
- `TestHandler_PostPhoto_MissingURL` — empty photo_url returns NonRetryableError
- `TestHandler_PostPhoto_VKError` — VK API error code 5 returns NonRetryableError

### Task 4: Orchestrator tool registration
- Registered `vk__post_photo` tool with `photo_url` (required), `caption`, `group_id` params
- Updated `vk__publish_post` description to clarify text-only and redirect to `vk__post_photo` for image posts

### Task 5: Client-side rate limiter (3 req/sec)
- Added `golang.org/x/time/rate` dependency
- Added `limiter *rate.Limiter` field to Client struct, initialized at `rate.NewLimiter(3, 1)`
- Added `wait()` helper using `context.Background()`
- All 9 public VK API methods now call `c.wait()` before making API requests

## Files modified
- `services/agent-vk/internal/agent/handler.go` — interface + switch case + postPhoto method
- `services/agent-vk/internal/vk/client.go` — PostPhoto impl + rate limiter
- `services/agent-vk/internal/agent/handler_test.go` — 3 new tests
- `services/orchestrator/cmd/main.go` — tool registration + description update
- `services/agent-vk/go.mod` / `go.sum` — added golang.org/x/time

## Verification
- `cd services/agent-vk && GOWORK=off go build ./...` — passes
- `cd services/agent-vk && GOWORK=off go test -race ./...` — all tests pass
- `cd services/orchestrator && GOWORK=off go build -o /dev/null ./cmd/main.go` — passes
