# SUMMARY 03-04: VK Community Info and Wall Read Tools

## Status: COMPLETE

## What was done

1. **VKClient interface** — Added `GetCommunityInfo(groupID string) (map[string]interface{}, error)` and `GetWallPosts(groupID string, count int) ([]map[string]interface{}, int, error)` to the interface in `handler.go`.

2. **VK SDK client implementation** — Implemented both methods in `client.go`:
   - `GetCommunityInfo` calls `GroupsGetByID` with fields `description,members_count,status,site,links,counters`, strips `-` prefix from groupID, returns name, screen_name, description, members_count, status, site, photo, and links.
   - `GetWallPosts` calls `WallGet` with owner_id and count, maps engagement stats (likes, comments, reposts, views) per post, returns posts array and total count.

3. **Handler dispatch** — Added `vk__get_community_info` and `vk__get_wall_posts` switch cases and handler methods:
   - Both validate that `group_id` is non-empty (NonRetryableError if missing).
   - `getWallPosts` defaults count to 10, clamps to max 100.
   - Both use `classifyVKError` for proper error taxonomy.

4. **Unit tests** — Added 7 tests:
   - `TestHandler_GetCommunityInfo` — success path with name, description, members_count
   - `TestHandler_GetCommunityInfo_MissingGroupID` — NonRetryableError
   - `TestHandler_GetCommunityInfo_VKError` — error code 100 classified as NonRetryableError
   - `TestHandler_GetWallPosts` — success with explicit count, posts array, and total
   - `TestHandler_GetWallPosts_DefaultCount` — no count arg defaults to 10
   - `TestHandler_GetWallPosts_ClampCount` — count 500 clamped to 100
   - `TestHandler_GetWallPosts_MissingGroupID` — NonRetryableError

5. **Orchestrator registration** — Registered both tools with `group_id` as optional (`"required": []string{}`). `get_wall_posts` also has optional `count` parameter.

## Files modified

- `services/agent-vk/internal/agent/handler.go` — interface + switch cases + handler methods
- `services/agent-vk/internal/vk/client.go` — SDK implementations
- `services/agent-vk/internal/agent/handler_test.go` — 7 new tests
- `services/orchestrator/cmd/main.go` — 2 tool registrations

## Verification

- `cd services/agent-vk && GOWORK=off go build ./...` — passes
- `cd services/agent-vk && GOWORK=off go test ./...` — all tests pass
- `cd services/orchestrator && GOWORK=off go build ./...` — passes
- 3 community info tests + 4 wall posts tests = 7 new tests total
