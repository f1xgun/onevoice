# Phase 3: VK Agent Completion — Research

**Researched:** 2026-03-19
**Scope:** VK-01 through VK-06, TST-01

---

## 1. Current State

### Existing VK Agent Code

**Handler** (`services/agent-vk/internal/agent/handler.go`):
- 3 tools implemented: `vk__publish_post`, `vk__update_group_info`, `vk__get_comments`
- `VKClient` interface: `PublishPost(groupID, text)`, `UpdateGroupInfo(groupID, description)`, `GetComments(groupID, count)`
- `VKClientFactory func(accessToken string) VKClient` — creates client per request from token
- `classifyVKError` — classifies VK API error codes (5=invalid token, 15=access denied, 100=invalid param, 113=invalid user as permanent; 6, 9 as rate-limited)
- `getClient` helper: fetches token via `TokenFetcher`, creates client, returns `(VKClient, groupID, error)`

**VK Client** (`services/agent-vk/internal/vk/client.go`):
- Wraps `vksdk/v3` (`github.com/SevereCloud/vksdk/v3 v3.2.2`)
- `PublishPost` calls `vk.WallPost` with `owner_id` and `message`
- `UpdateGroupInfo` calls `vk.GroupsEdit` with `group_id` (strips `-` prefix) and `description`
- `GetComments` calls `vk.WallGetComments` — maps items to `[]map[string]interface{}`

**Orchestrator Registration** (`services/orchestrator/cmd/main.go`):
- 3 VK tools registered under `a2a.AgentVK`:
  - `vk__publish_post` — text, group_id
  - `vk__update_group_info` — group_id, description
  - `vk__get_comments` — group_id, count

### What Needs to Be Added

6 new tools total (some replace/extend existing ones):

| Requirement | Tool Name | New/Modify | VK API Methods |
|---|---|---|---|
| VK-01 | `vk__post_photo` | New | `photos.getWallUploadServer` + upload + `photos.saveWallPhoto` + `wall.post` |
| VK-02 | `vk__schedule_post` | New | `wall.post` with `publish_date` |
| VK-03 | `vk__reply_comment` | New | `wall.createComment` |
| VK-04 | `vk__delete_comment` | New | `wall.deleteComment` |
| VK-05 | `vk__get_community_info` | New | `groups.getById` with `fields` |
| VK-06 | `vk__get_wall_posts` | New | `wall.get` |

---

## 2. VK API Details Per Tool

### VK-01: `vk__post_photo` — Photo Post (Two-Step Upload)

**Step 1: Get upload server**
- SDK: `vk.PhotosGetWallUploadServer(Params{"group_id": groupID})`
- Returns: `PhotosGetWallUploadServerResponse` with `UploadURL` string

**Step 2: Upload file to upload URL**
- SDK: `vk.UploadFile(uploadURL, file, "photo", "photo.jpeg")` — multipart POST
- Returns: raw JSON bytes with `server`, `photo`, `hash` fields

**Step 3: Save wall photo**
- SDK: `vk.PhotosSaveWallPhoto(Params{"server": ..., "photo": ..., "hash": ..., "group_id": groupID})`
- Returns: `PhotosSaveWallPhotoResponse` = `[]PhotosPhoto`
- `PhotosPhoto` has `OwnerID` (int) and `ID` (int)

**Step 4: Create wall post with attachment**
- SDK: `vk.WallPost(Params{"owner_id": ownerID, "message": caption, "attachments": "photo{ownerID}_{photoID}"})`
- Attachment format: `photo{owner_id}_{photo_id}` (e.g., `photo-123456_789012`)
- Returns: `WallPostResponse{PostID int}`

**SDK shortcut:** `vk.UploadGroupWallPhoto(groupID int, file io.Reader)` wraps steps 1-3 into a single call. This is the preferred approach — the SDK handles the three-step dance internally.

**Agent flow:** Download image from URL -> `UploadGroupWallPhoto` -> format attachment string -> `WallPost` with attachment + message.

**Tool args:** `photo_url` (string, required), `caption` (string), `group_id` (string)
**Response:** `{"post_id": int}`

### VK-02: `vk__schedule_post` — Scheduled Post

- SDK: `vk.WallPost(Params{"owner_id": ownerID, "message": text, "publish_date": unixTimestamp})`
- `publish_date` — Unix timestamp for future publication. VK requires it to be at least current time + a few minutes.
- The post is created with `post_type: "postponed"` internally by VK when `publish_date` is set.
- Can include attachments (photo, etc.) same as regular posts.

**Tool args:** `text` (string, required), `publish_date` (string, required — ISO 8601 or Unix timestamp), `group_id` (string)
**Response:** `{"post_id": int}`

### VK-03: `vk__reply_comment` — Reply to Comment

- SDK: `vk.WallCreateComment(Params{"owner_id": ownerID, "post_id": postID, "message": text, "reply_to_comment": commentID})`
- Returns: `WallCreateCommentResponse{CommentID int, ParentsStack []int}`
- `reply_to_comment` makes it a threaded reply. If omitted, it's a top-level comment.

**Tool args:** `post_id` (number, required), `comment_id` (number, required — the comment being replied to), `text` (string, required), `group_id` (string)
**Response:** `{"comment_id": int}`

### VK-04: `vk__delete_comment` — Delete Comment

- SDK: `vk.WallDeleteComment(Params{"owner_id": ownerID, "comment_id": commentID})`
- Returns: `int` (1 on success)
- Requires community admin/moderator permissions.

**Tool args:** `comment_id` (number, required), `group_id` (string)
**Response:** `{"status": "deleted"}`

### VK-05: `vk__get_community_info` — Community Info

- SDK: `vk.GroupsGetByID(Params{"group_id": groupID, "fields": "description,members_count,status,site,links,counters"})`
- Returns: `GroupsGetByIDResponse{Groups []GroupsGroup}`
- `GroupsGroup` fields: `ID`, `Name`, `ScreenName`, `Description`, `MembersCount`, `Status`, `Site`, `Links []GroupsLinksItem`, `Photo200`

**Tool args:** `group_id` (string, required)
**Response:** `{"name": str, "description": str, "members_count": int, "status": str, "site": str, "screen_name": str, "links": [...]}`

### VK-06: `vk__get_wall_posts` — Wall Posts Read

- SDK: `vk.WallGet(Params{"owner_id": ownerID, "count": count, "offset": offset})`
- Returns: `WallGetResponse{Count int, Items []WallWallpost}`
- `WallWallpost` fields: `ID`, `Text`, `Date` (unix), `Comments.Count`, `Likes.Count`, `Reposts.Count`, `Views.Count`, `Attachments`
- Default count: 20, max: 100. Offset-based pagination.

**Tool args:** `group_id` (string, required), `count` (number, default 10)
**Response:** `{"total": int, "posts": [{"id": int, "text": str, "date": int, "likes": int, "comments": int, "reposts": int, "views": int}, ...]}`

---

## 3. Two-Step Photo Upload Flow

The VK SDK (`vksdk/v3`) provides `UploadGroupWallPhoto(groupID int, file io.Reader)` which encapsulates the full three-step flow internally:

1. `PhotosGetWallUploadServer(Params{"group_id": groupID})` — gets temporary upload URL
2. `UploadFile(uploadURL, file, "photo", "photo.jpeg")` — HTTP multipart POST to VK's upload server
3. `PhotosSaveWallPhoto(Params{"server": ..., "photo": ..., "hash": ..., "group_id": groupID})` — saves and returns `[]PhotosPhoto`

**What the agent must do:**
1. Download the image from `photo_url` (HTTP GET) into an `io.Reader`
2. Parse `group_id` to int (strip `-` prefix)
3. Call `UploadGroupWallPhoto(groupID, reader)` — returns `[]PhotosPhoto`
4. Format attachment string: `fmt.Sprintf("photo%d_%d", photos[0].OwnerID, photos[0].ID)`
5. Call `WallPost` with `owner_id`, `message`, `attachments`

**VKClient interface change:** Add `PostPhotoWall(groupID int, file io.Reader, caption string) (int64, error)` that wraps steps 2-5.

Alternatively, keep it more granular:
- `UploadWallPhoto(groupID int, file io.Reader) (ownerID, photoID int, error)`
- Then handler combines with `PublishPost` + attachment

The single-method approach is cleaner for the handler and more testable (mock one method).

---

## 4. Scheduling via `publish_date`

VK's `wall.post` accepts `publish_date` as a Unix timestamp parameter. When present:
- VK creates the post with status "postponed"
- VK automatically publishes at the specified time
- No local scheduler/cron needed
- **Constraint:** `publish_date` must be in the future (at least ~2 minutes ahead per VK docs)
- **Constraint:** Community can have max 150 scheduled posts
- The post ID is returned immediately (can be used to cancel/edit)

**Implementation approach:** The `vk__schedule_post` tool reuses `wall.post` with `publish_date` added. The handler should:
1. Parse the date (accept ISO 8601 string, convert to Unix timestamp)
2. Validate it's in the future
3. Call `WallPost` with `owner_id`, `message`, `publish_date`

This can share the same VKClient method as `PublishPost` but with an optional `publishDate` parameter, or be a separate method `SchedulePost(groupID, text string, publishDate int64) (int64, error)`.

---

## 5. How to Register New Tools in Orchestrator

In `services/orchestrator/cmd/main.go`, function `registerPlatformTools`:

1. Add new `llm.ToolDefinition` entries to the VK agent's tools slice
2. Each tool needs:
   - `Type: "function"`
   - `Function.Name` — must match exactly what the handler's `switch` expects (e.g., `vk__post_photo`)
   - `Function.Description` — in Russian, clear and mutually exclusive from other tools
   - `Function.Parameters` — JSON Schema object with `properties` and `required`
3. The NATS executor is automatically created per tool: `natsexec.New(a2a.AgentVK, toolName, conn)`

**New registrations needed (6 tools):**
- `vk__post_photo` — photo_url (required), caption, group_id
- `vk__schedule_post` — text (required), publish_date (required), group_id
- `vk__reply_comment` — post_id (required), comment_id (required), text (required), group_id
- `vk__delete_comment` — comment_id (required), group_id
- `vk__get_community_info` — group_id (required)
- `vk__get_wall_posts` — group_id, count

**Existing tools to keep or replace:**
- `vk__publish_post` — keep (text-only post). Differentiate description from `vk__post_photo`.
- `vk__update_group_info` — keep (already works).
- `vk__get_comments` — keep (already works). Different from `vk__reply_comment` and `vk__delete_comment`.

Total VK tools after phase: 9 (3 existing + 6 new).

---

## 6. Mock Server Design for Integration Tests

### Pattern: `httptest.Server` with VK-compatible responses

VK API returns JSON in the format `{"response": {...}}` on success and `{"error": {"error_code": N, "error_msg": "..."}}` on failure.

The SDK's `vk.RequestUnmarshal` calls `https://api.vk.com/method/{method}` (or custom base URL). The SDK supports setting a custom base URL — **must verify this**.

**VK SDK base URL override:**
```go
vk := vkapi.NewVK(token)
// vksdk/v3 does not expose a simple BaseURL field.
// Check if Params or VK struct has a method for custom endpoint.
```

**Alternative approach:** Since the `VKClient` interface abstracts all VK operations, integration tests can mock at the interface level (like existing `handler_test.go`). For deeper integration tests that test the real `vk/client.go`, we need to intercept HTTP calls.

**Recommended mock server strategy:**
1. Create `httptest.Server` that routes on `method` query param or path
2. The vksdk client needs its internal HTTP client or base URL redirected to the test server
3. Alternatively, if vksdk supports a custom `http.Client`, inject one with the test server's URL

**Checking vksdk customization:**

The `vkapi.VK` struct likely has `Client *http.Client` and/or a base URL. For the mock server to work:
- Option A: Pass a custom `http.Client` with transport that redirects to test server
- Option B: Use `VK.Init()` or constructor that accepts options
- Option C: Mock at the `VKClient` interface level (simpler, already done in unit tests)

**For TST-01:** The CONTEXT.md specifies `httptest.Server` with VK-compatible JSON responses. This means testing through the real HTTP layer, not just interface mocks. We need to:
1. Create a mock VK API server that handles all 6+ methods
2. Route the vksdk client to use this server
3. Test the full `client.go` + `handler.go` path

**Mock endpoints needed:**
- `wall.post` — return `{"response": {"post_id": N}}`
- `wall.get` — return `{"response": {"count": N, "items": [...]}}`
- `wall.createComment` — return `{"response": {"comment_id": N}}`
- `wall.deleteComment` — return `{"response": 1}`
- `wall.getComments` — return `{"response": {"count": N, "items": [...]}}`
- `groups.getById` — return `{"response": {"groups": [...]}}`
- `photos.getWallUploadServer` — return `{"response": {"upload_url": "http://testserver/upload"}}`
- Upload endpoint — accept multipart, return `{"server": 1, "photo": "...", "hash": "..."}`
- `photos.saveWallPhoto` — return `{"response": [{"id": N, "owner_id": M}]}`

**Error scenarios:**
- Error 5 (invalid token) — `{"error": {"error_code": 5, "error_msg": "User authorization failed"}}`
- Error 6 (rate limit) — `{"error": {"error_code": 6, "error_msg": "Too many requests per second"}}`
- Network timeout — test server closes connection

---

## 7. Dependencies and Ordering

### Build Order

```
PLAN-3.1: vk__post_photo (VK-01)
  └── Extends VKClient interface with photo upload method
  └── Adds handler case
  └── Registers in orchestrator

PLAN-3.2: vk__schedule_post (VK-02)
  └── Extends VKClient interface with schedule method
  └── Can reuse wall.post path from publish_post
  └── Registers in orchestrator

PLAN-3.3: vk__reply_comment + vk__delete_comment (VK-03, VK-04)
  └── Extends VKClient interface with 2 methods
  └── Adds 2 handler cases
  └── Registers in orchestrator

PLAN-3.4: vk__get_community_info + vk__get_wall_posts (VK-05, VK-06)
  └── Extends VKClient interface with 2 methods
  └── Adds 2 handler cases
  └── Registers in orchestrator

PLAN-3.5: Integration tests (TST-01)
  └── Depends on all 4 plans above being complete
  └── Mock VK API server
  └── Tests all tools through real HTTP layer
```

**Plans 3.1-3.4 are independent of each other** — they can be implemented in any order. Each adds new methods to the `VKClient` interface and new cases to the handler switch. No conflicts.

**Plan 3.5 depends on 3.1-3.4** — integration tests need all tools implemented.

### Files Modified Per Plan

All plans touch:
- `services/agent-vk/internal/agent/handler.go` — new switch cases
- `services/agent-vk/internal/vk/client.go` — new VKClient methods
- `services/orchestrator/cmd/main.go` — new tool registrations

Plan 3.5 additionally creates:
- `services/agent-vk/internal/vk/client_test.go` (or similar) — integration tests with mock server

---

## 8. Risks and Gotchas

### VK API Quirks

1. **`owner_id` is negative for communities.** `wall.post` and `wall.get` use `owner_id = -groupID`. But `groups.edit`, `groups.getById`, and `photos.getWallUploadServer` use positive `group_id`. The existing code already handles this (strips `-` in `UpdateGroupInfo`). New tools must be consistent.

2. **`wall.getComments` requires `post_id`.** The current `GetComments` implementation passes `owner_id` without `post_id`, which may fail. The VK API requires `owner_id` + `post_id`. This is a bug in existing code that should be addressed.

3. **Photo upload returns an array.** `PhotosSaveWallPhoto` returns `[]PhotosPhoto`. Must check `len > 0` before accessing `[0]`.

4. **`publish_date` minimum offset.** VK requires scheduled posts to be at least ~120 seconds in the future. Attempting to schedule in the past or too soon returns error 100 (invalid parameter).

5. **Scheduled post limit.** Communities can have a maximum of 150 postponed posts. Error 214 is returned when the limit is reached.

6. **`wall.deleteComment` permissions.** Only community admins/moderators can delete comments on community walls. The VK access token must have `wall` permission scope.

7. **Rate limiting (3 req/sec).** VK enforces ~3 requests per second per access token. The photo upload flow uses 3 API calls (getUploadServer + saveWallPhoto + wallPost) plus 1 HTTP upload. A single `vk__post_photo` call can hit the rate limit if other calls are in flight. The SDK may or may not have built-in rate limiting.

8. **`groups.getById` changed in v5.199.** The method now returns `{"groups": [...], "profiles": [...]}` instead of a flat array. The vksdk v3.2.2 already uses `GroupsGetByIDResponse{Groups, Profiles}`, so this is handled.

9. **`UploadFile` requires a real io.Reader.** The photo upload flow downloads the image from a URL. Must handle: invalid URL, non-image content type, oversized files (VK limit: 50MB), slow downloads (timeout).

10. **Community token vs. user token.** Some methods require a user token (e.g., `groups.edit`), while `wall.post` can work with a community token. The token stored per-integration should be a user token with appropriate scopes for full functionality.

### Testing Risks

11. **VK SDK base URL.** If vksdk does not support overriding the API base URL, integration tests will need to use a custom `http.Client` with a transport that rewrites requests. The `vkapi.VK` struct has a `Client` field (`*http.Client`) and a `MethodURL` field that can be overridden — **must verify at implementation time.**

12. **Multipart upload in tests.** The photo upload mock must parse multipart form data and return the expected JSON format. This adds complexity but is essential for VK-01 coverage.

### Dependency Risks

13. **vksdk v3.2.2 coverage.** All needed methods (`WallPost`, `WallGet`, `WallCreateComment`, `WallDeleteComment`, `GroupsGetByID`, `UploadGroupWallPhoto`) are present in v3.2.2. No version bump needed.

14. **No new Go dependencies required.** All needed packages are already in go.mod.

---

## 9. Validation Architecture

### Input Validation (in handler, before VK API call)

Each handler method should validate required args before making API calls:

| Tool | Required Args | Validation |
|---|---|---|
| `vk__post_photo` | `photo_url` | Non-empty, valid URL format |
| `vk__schedule_post` | `text`, `publish_date` | Non-empty text; date parseable and in future |
| `vk__reply_comment` | `post_id`, `comment_id`, `text` | Numeric IDs > 0; non-empty text |
| `vk__delete_comment` | `comment_id` | Numeric ID > 0 |
| `vk__get_community_info` | `group_id` | Non-empty |
| `vk__get_wall_posts` | `group_id` | Non-empty; count 1-100 |

**Validation errors should be `NonRetryableError`** — invalid input is permanent, retrying won't help.

### Error Classification (unchanged)

The existing `classifyVKError` handles all VK error codes correctly. New tools will reuse it via the same `classifyVKError(err)` wrapping pattern.

### Response Validation

VK API responses are typed by the SDK. Key checks:
- `UploadGroupWallPhoto` returns `[]PhotosPhoto` — check `len > 0`
- `GroupsGetByID` returns `GroupsGetByIDResponse{Groups: [...]}` — check `len(Groups) > 0`
- Numeric returns from `WallDeleteComment` — check `== 1` for success

### VKClient Interface Extension

The `VKClient` interface needs 6 new methods:

```go
type VKClient interface {
    // Existing
    PublishPost(groupID, text string) (int64, error)
    UpdateGroupInfo(groupID, description string) error
    GetComments(groupID string, count int) ([]map[string]interface{}, error)

    // New for Phase 3
    PostPhoto(groupID string, photoURL, caption string) (int64, error)
    SchedulePost(groupID, text string, publishDate int64) (int64, error)
    ReplyComment(groupID string, postID, commentID int, text string) (int, error)
    DeleteComment(groupID string, commentID int) error
    GetCommunityInfo(groupID string) (map[string]interface{}, error)
    GetWallPosts(groupID string, count int) ([]map[string]interface{}, int, error)
}
```

Each new method will be implemented in `services/agent-vk/internal/vk/client.go` using the corresponding vksdk calls.

---

## RESEARCH COMPLETE
