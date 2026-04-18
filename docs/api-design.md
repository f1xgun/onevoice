# API Design — OneVoice REST Conventions

Applies to `services/api/` (port 8080). The orchestrator's SSE endpoint (`/chat/{id}`) is documented in [architecture.md](architecture.md) and [../services/orchestrator/AGENTS.md](../services/orchestrator/AGENTS.md).

---

## URL Conventions

```
GET    /api/v1/users/{id}                 → 200 + User JSON
POST   /api/v1/users                      → 201 + User JSON (with Location header)
PUT    /api/v1/users/{id}                 → 200 + User JSON
DELETE /api/v1/users/{id}                 → 204 (no body)

GET    /api/v1/businesses/{id}/integrations       → 200 + Integration[]
POST   /api/v1/integrations/{platform}/connect    → 302 redirect to OAuth
```

**Rules:**

- Version the API — `/api/v1/`.
- Plural nouns for collections (`/users`, not `/user`).
- Use HTTP verbs correctly: `GET` = read-only, `POST` = create, `PUT` = replace, `PATCH` = partial update, `DELETE` = delete.
- Return proper status codes (see table below).

## Status Codes

| Code | When |
|------|------|
| 200 | Successful read or update |
| 201 | Successful create (include `Location` header) |
| 204 | Successful delete (no body) |
| 302 | OAuth redirect |
| 400 | Validation error / malformed input |
| 401 | Missing or expired token |
| 403 | Valid token, insufficient permissions |
| 404 | Resource not found |
| 409 | Conflict (e.g. duplicate email) |
| 429 | Rate limit exceeded (include `Retry-After`) |
| 500 | Internal server error (log full detail, do not expose to client) |
| 501 | Not yet implemented (stub endpoint) |

## Error Response Body

Consistent JSON shape for all errors:

```json
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "Validation failed",
    "details": {
      "email": "Invalid email format",
      "phone": "Phone number required"
    }
  }
}
```

- `code` — stable machine-readable identifier; safe to switch on in clients.
- `message` — human-readable; OK to surface in UI.
- `details` — optional field-level context.

## Success Response Body

Direct JSON, no envelope:

```json
{ "id": "uuid...", "email": "a@b.com", ... }
```

Collections return an array or a paginated object, never wrap in `{ "data": ... }`.

## Authentication

- **Access token**: JWT (HMAC-SHA256), 15-minute expiry, sent via `Authorization: Bearer <token>`. Stateless — no DB lookup to validate.
- **Refresh token**: random UUID, SHA-256 hashed, Redis-backed, 7-day TTL. Rotated on every use.
- **Claims**: `UserID` (UUID), `Email`, `Role`, `ExpiresAt`, `IssuedAt`.

See `services/api/internal/middleware/AGENTS.md` style — the middleware README at `services/api/internal/middleware/README.md` has deeper detail on auth, CORS, rate-limit, and logging middleware.

## Domain → HTTP Error Mapping

Handled centrally by the handler layer:

| Domain Error | HTTP Status |
|---|---|
| `ErrUserNotFound`, `ErrBusinessNotFound` | 404 |
| `ErrUserExists` | 409 |
| `ErrInvalidCredentials` | 401 |
| `ErrUnauthorized`, `ErrInvalidToken` | 401 |
| `ErrForbidden` | 403 |
| Validation errors (from `validator/v10`) | 400 |
| Rate-limit errors (`ErrRateLimitExceeded`) | 429 |
| Anything else | 500 (logged, details not exposed) |

## Async Operations

Sync/bulk operations (e.g. "apply change to all connected platforms") return a `task_id` and process in the background. UI polls a status endpoint per task. Task records live in MongoDB `tasks` collection.

## Rate Limiting

- **API level** (Redis, per-IP + per-endpoint): see `services/api/internal/middleware/ratelimit.go`.
- **LLM Router level** (Redis, per-user + tier): see `pkg/llm/ratelimit.go`.
- **Platform level**: each agent respects its platform's limits (VK quotas, Telegram 429 + `retry_after`).

## Pagination

- Default page size: 20; max: 100.
- Query params: `?limit=20&offset=0` (offset pagination) or `?limit=20&after=<cursor>` (cursor pagination when ordering is stable).
- Include total count in responses only when cheap; otherwise prefer "has_more" boolean.
