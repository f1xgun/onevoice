# Conversation handler

`services/api/internal/handler/conversation.go` implements the conversation
CRUD surface plus the move / pin / unpin / list-messages endpoints. The
chat-turn lifecycle (orchestrator SSE proxy, message persistence) lives in
`chat_proxy.go`; this file is the conversation-document side of the same
business resource.

## Routes

```
POST    /api/v1/conversations
GET     /api/v1/conversations
GET     /api/v1/conversations/{id}
PUT     /api/v1/conversations/{id}
DELETE  /api/v1/conversations/{id}
GET     /api/v1/conversations/{id}/messages
POST    /api/v1/conversations/{id}/move
POST    /api/v1/conversations/{id}/pin
POST    /api/v1/conversations/{id}/unpin
```

`regenerate-title` is mounted by a different handler (`titler.go`) when
the titler service is wired. See [docs/api/handlers/titler.md](titler.md).

## Construction

`NewConversationHandler` is constructed from five required collaborators:

- `domain.ConversationRepository` — Mongo-backed CRUD on the
  `conversations` collection.
- `domain.MessageRepository` — used indirectly via
  `ConversationService.OpenChat` (the four-op composite that powers
  `ListMessages`).
- `BusinessService` — retained for wire compatibility with the legacy
  five-arg signature; the handler itself does not consult it directly
  any more.
- `ProjectService` — used by `CreateConversation` to validate that a
  supplied `projectId` belongs to the caller's business BEFORE the
  insert. Move-conversation goes through `ConversationService.MoveToProject`
  which has its own project lookup.
- `ConversationService` — owns the multi-repo composites (`OpenChat`
  for `ListMessages`, `MoveToProject` for `MoveConversation`).

Nil deps return `fmt.Errorf` from the constructor — wire fails loud at
startup.

## Constants

- `DefaultConversationLimit = 20` — page size when `?limit=` is absent.
- `MaxConversationLimit = 100` — server-side ceiling. Higher values are
  silently clamped (not rejected) so a frontend bug never blocks the
  list view; honest fix lives client-side.
- `mongoObjectIDHexLen = 24` — guards against accidentally hitting the
  pool with a path param that obviously cannot be a Mongo ObjectID
  (returns 400 BEFORE the driver attempts a hex parse).

## ACL pattern

Every handler follows the same six-line preamble:

```go
bc, ok := authz.BusinessContextFromCtx(r.Context())
if !ok {
    slog.ErrorContext(..., "<Handler>: no BusinessContext in ctx — middleware misconfiguration")
    writeJSONError(w, 500, "internal_server_error")
    return
}
if !authz.Can(r.Context(), authz.Perm<Verb>) {
    writeJSONError(w, 403, "forbidden")
    return
}
```

Permission lattice per route:

| Route                                | Permission             |
|--------------------------------------|------------------------|
| `POST /conversations`                | `PermContentCreate`    |
| `GET /conversations`                 | `PermContentRead`      |
| `GET /conversations/{id}`            | `PermContentRead`      |
| `PUT /conversations/{id}`            | `PermContentUpdate`    |
| `DELETE /conversations/{id}`         | `PermContentDelete`    |
| `GET /conversations/{id}/messages`   | `PermContentRead`      |
| `POST /conversations/{id}/move`      | `PermContentUpdate`    |
| `POST /conversations/{id}/pin`       | `PermContentUpdate`    |
| `POST /conversations/{id}/unpin`     | `PermContentUpdate`    |

Pin / unpin are `PermContentUpdate` because they mutate metadata on a
conversation the user already owns; they do NOT need create or delete
because no new conversation is materialised.

## Cross-tenant defense

Three layers of defense, depending on the route:

1. **Owner-only routes (GET/PUT/DELETE single conversation)** —
   `conversation.UserID != bc.UserID.String()` after the GET → 403. Note
   this is one of the rare places the codebase returns 403 rather than
   uniform 404; the existence has already been confirmed by the GET,
   so the existence-leak invariant does not apply here.
2. **List route** — `ListByUserID(bc.UserID, …)` SQL/Mongo filter; no
   conversation owned by another user can be returned regardless of
   any supplied query parameters.
3. **Pin/unpin** — the repository `Pin` / `Unpin` methods take
   `(conversationID, businessID, userID)` and compose the predicate
   into the update filter directly. Cross-tenant attempts surface as a
   uniform 404 (`domain.ErrConversationNotFound`); the handler never
   leaks whether the conversation belongs to another business. This is
   the industry-standard guard against existence enumeration and the
   one place we DO follow the uniform-404 rule for this resource.

## Projection shapes

### Conversation document

The wire shape is the `domain.Conversation` struct verbatim. Newly
created chats start unpinned: `PinnedAt` is `nil` (the SINGLE source of
truth for the unpinned state). The legacy `Pinned bool` field was
removed from the struct; do not re-introduce it — the codebase has
moved to "presence of `pinned_at` IS the pinned signal" and round-trip
serialisers everywhere depend on that invariant.

### CreateConversationRequest

`ProjectID *string`: both an explicit JSON `null` and an absent
`projectId` key map to Go's `*string = nil` (standard `encoding/json`
semantics). Downstream effect in both cases: conversation persisted
with `project_id = null` (the "Без проекта" bucket). The handler does
NOT distinguish the two cases — intentional, matches Go idiom.

When `req.ProjectID != nil && *req.ProjectID != ""` the handler
validates that the project exists AND belongs to the caller's business:

- `uuid.Parse` failure → `400 invalid project id`.
- `ProjectService.GetByID` returning `domain.ErrProjectNotFound` →
  `404 project not found` (NOT 403 — per `docs/security.md` we do not
  leak existence via 403 here).
- Any other error → 500.

### MoveConversationRequest

`ProjectID *string`: same three-way collapse — null / empty / absent
all move the chat into the virtual "Без проекта" bucket.
`ConversationService.MoveToProject` is the destination; it owns:

- Ownership check (`conversation.UserID == bc.UserID`).
- Cross-business project lookup (404 via `domain.ErrProjectNotFound`).
- The atomic `UpdateProjectAssignment` write.
- The visible system-role note append. Copy is byte-exact:
  `"[Чат перемещён в «{destination}» — с этого момента применяется новая политика]"`
  where `{destination}` is the new project's name or `"Без проекта"`.
- The post-commit conversation re-fetch.

The system-note append is best-effort. If the message-create fails the
move itself already landed, so the service logs and the handler still
returns success. Rolling back the move on a note-append failure would
be more surprising than a missing note.

Error mapping:

| Service error                      | HTTP |
|------------------------------------|------|
| `domain.ErrConversationNotFound`   | 404  |
| `domain.ErrForbidden`              | 403  |
| `domain.ErrProjectNotFound`        | 404  |
| `service.ErrInvalidProjectID`      | 400  |
| anything else                      | 500  |

### ListMessages → ChatView

`GET /conversations/{id}/messages` is a pure HTTP-mapping adapter over
`ConversationService.OpenChat`. The service owns the four-op composite:
conversation fetch + ownership gate + messages list + pending-batch
projection. The returned `ChatView` JSON-encodes byte-identical to the
pre-migration `listMessagesResponse` shape — frontend zod schemas
remain unchanged.

Sentinel mapping:

| Service error                      | HTTP |
|------------------------------------|------|
| `domain.ErrConversationNotFound`   | 404  |
| `domain.ErrForbidden`              | 403  |
| anything else                      | 500  |

### UpdateConversation

`PUT /conversations/{id}` accepts a `{title}` body and is the
"manual rename" path. The handler unconditionally sets
`TitleStatus = TitleStatusManual` so the auto-titler's gate (in
`titler.go`) will NEVER overwrite the user's chosen title on a
subsequent turn. The repo's `Update` persists this in the same `$set`
block as the title itself.

## Pin / unpin atomicity

`Pin` and `Unpin` both go through the repository's pin-method (NOT
through a generic `Update`):

- `Pin(conversationID, businessID, userID)` does
  `db.collection.updateOne({_id, business_id, user_id}, {$set: {pinned_at: now}})`.
- `Unpin` is symmetric with `$set: {pinned_at: null}`.

The `(business_id, user_id)` predicate is the cross-tenant defense
(Pitfalls §19) — defense-in-depth on top of the `RequireBusinessAccess`
middleware. No `RowsAffected == 0` round-trip is needed because the
handler refetches the conversation immediately after and the refetch
either returns the updated row or surfaces `ErrConversationNotFound`
which the handler maps to 404.

The handler validates the path param twice:

- `len(conversationID) != mongoObjectIDHexLen` → fast 400 BEFORE any
  driver call.
- `primitive.ObjectIDFromHex(conversationID)` error → 400. Catches
  same-length non-hex strings.

## Error hygiene

`writeJSONError(w, 500, "internal server error")` is the canonical
opaque shape. Repo errors are logged at ERROR with `error`,
`conversation_id`, `user_id`, `business_id` and the slog correlation ID
(auto-attached by the slog middleware); they are NEVER surfaced to the
client because a `pgx`/`mongo` error message would disclose SQL/Mongo
shape and schema metadata to a probing caller.
