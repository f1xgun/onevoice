# Audit-log handler

`services/api/internal/handler/audit_log.go` implements the read path for
business audit logs:

```
GET /api/v1/businesses/{id}/audit-logs
```

The handler is deliberately stateless beyond its repository reference: no
per-request caches, no in-memory rate limits (global middleware handles
those), no body parsing (GET-only).

## ACL checks

1. `BusinessContextFromCtx` — missing context returns 500. That branch is
   only reachable if a future regression registers this route outside
   the `/businesses/{id}` subtree. `lint-rbac` catches that at PR time;
   the runtime 500 here is the belt-and-braces backstop.
2. `authz.Can(PermAuditRead)` — 403 forbidden if missing. The system
   `Owner` and `Admin` roles hold this permission by seed; other roles
   do not.
3. `RequireBusinessAccess` (on the parent route) — already enforced
   membership and tenant scoping before the handler runs. Non-members
   get 404; suspended members get 403 `forbidden_suspended`.

The handler does not need its own tenant filter — every repo call passes
`bc.BusinessID` as a SQL `WHERE` clause, so a leaked or spoofed `actor_id`
in the query string still cannot return another tenant's rows.

## Narrow repo interface

`AuditLogLister` is a one-method interface (`ListByBusinessWithActors`)
exported so the wire layer can type-assert the `domain.AuditLogRepository`
into this narrow shape. The handler does NOT depend on the full
`domain.AuditLogRepository` because:

- `ListByBusinessWithActors` returns `[]repository.AuditLogRow` — a
  repository-package type carrying join columns (`ActorEmail`,
  `ActorDisplayName`) that intentionally are NOT part of the domain
  surface.
- Stubbing one method in unit tests is cheaper than implementing
  `Insert` / `DeleteOlderThan` / `ListByBusiness`.

The single-query LEFT JOIN is load-bearing: a naive handler that called
`UserRepository.GetByID` per row would re-introduce the N+1 anti-pattern
that the join exists to prevent.

## Filter shape

All filter fields are optional query parameters. Each one validates at the
boundary and returns a discriminator-specific 400:

| Param      | Validation                                                | Bad input            |
|------------|-----------------------------------------------------------|----------------------|
| `category` | Must be in `knownCategories` (closed set)                 | `400 invalid_category` |
| `action`   | Must be in `knownActions` (closed set)                    | `400 invalid_action`   |
| `actor`    | `uuid.Parse`                                              | `400 invalid_actor`    |
| `from`     | `time.Parse(RFC3339)`                                     | `400 invalid_date`     |
| `to`       | `time.Parse(RFC3339)`                                     | `400 invalid_date`     |
| `cursor`   | `audit.DecodeCursor` (base64 → JSON → time+uuid)          | `400 invalid_cursor`   |
| `limit`    | `strconv.Atoi`, range `[1, maxPageLimit]` (200)           | `400 invalid_limit`    |

`knownActions` and `knownCategories` are explicit closed sets rather than
"reflect anything that parses". The drift surface is small (21 actions,
5 categories today) and the failure mode is explicit: a frontend typo
fails at the API boundary with a structured error, not silently against
an empty result set. Adding a new audit action requires the const +
builder in `pkg/audit/`, the call site wiring, AND an entry here. The
linter doesn't enforce the third step today — code review does.

`knownCategories` deliberately excludes `"other"` even though
`audit.ActionCategory` will return it for an action with an unknown
prefix. No live action emits an unknown prefix; accepting `"other"` would
give a caller a way to probe for prefix-drift in the registry.

## Pagination (cursor)

The endpoint uses opaque cursor pagination:

- `pkg/audit.EncodeCursor(createdAt, id)` packs a `(time, uuid)` tuple as
  base64-JSON. The repo's keyset query (`WHERE (created_at, id) < (?, ?)`)
  uses both columns so a tie on `created_at` is broken deterministically
  by `id`.
- `pkg/audit.DecodeCursor` unwraps every "bad input" cause (empty /
  non-base64 / non-JSON / missing fields / bad UUID / bad RFC3339) into
  the single `ErrInvalidCursor` sentinel. The handler maps that to one
  400 — distinguishing them client-side would only help an attacker probe
  the cursor format.
- `NextCursor` is non-nil ONLY when the returned page is full. A short
  page signals end-of-stream → `next_cursor: null` and the frontend's
  "Load more" button hides itself.
- A page that is exactly `limit`-sized AND happens to be the last page
  returns a non-nil cursor; the next request gets an empty page and stops.
  That is one wasted round-trip per stream-end — accepted in exchange for
  not over-fetching a `+1` row sentinel.

Defaults: `defaultPageLimit = 50`, `maxPageLimit = 200`. The repo also
clamps internally so a malformed handler cannot trigger an unbounded scan;
the handler returns 400 BEFORE hitting the repo to give the frontend a
fast feedback loop.

## DTO mapping

The wire shape (`AuditLogDTO`) keeps pointer fields with `omitempty`-
friendly nullability so the JSON encoder emits `null` rather than
zero-strings for missing actors / business / display names. The frontend
zod schema accepts both `null` and absent.

`Details` is passed through as `json.RawMessage` so each builder's typed
`Details` struct (`pkg/audit/details.go`) round-trips byte-for-byte
without the handler re-parsing the JSONB column.

The `ActorEmail == "" → nil` mapping has two causes:

- `audit_logs.user_id IS NULL` (failed-login row, no logged-in actor).
- The LEFT JOIN matched no `users` row (the actor has since been deleted).

Either way, the JSON contract surfaces `actor_email: null`. The frontend
renders `"Неизвестен ({attempted_email})"` using `details.attempted_email`
in the failed-login case.

## Retention boundary

The handler does NOT enforce retention — that is a write-side concern
handled by `pkg/audit.DeleteOlderThan` invoked from the policy-sweep
background job. The read path returns whatever rows still exist;
post-retention deletes simply disappear from the keyset window. A request
spanning the retention boundary degrades silently into "fewer rows than
the user expected", which is the documented behaviour: the contract is
"the last N days of audit history", not "all audit events ever".

## Error hygiene

Repo errors are wrapped `pgx` errors or connection drops. The handler
NEVER surfaces them to the client (they would disclose SQL shape and
schema metadata to a probing caller). Instead the handler logs with
`slog.ErrorContext` carrying `error`, `business_id`, and the correlation
ID (attached automatically by the slog middleware), then returns
`500 internal_server_error`.
