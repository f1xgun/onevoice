# Server value telemetry

Canonical business completions that reach the telemetry writer are durably
stored in `telemetry_events` with `event_type = value`. Emission into that
writer remains bounded best-effort, as described below. The closed server
action set is:

| Action | Completion boundary | Metadata |
|---|---|---|
| `signup_completed` | User transaction or legacy user insert committed | none |
| `email_verified` | Verification token consumption and user update committed | none |
| `org_created` | Business and owner membership transaction committed | none |
| `integration_connected` | Encrypted integration row persisted | `platform`, `kind=integration` |
| `post_published` | Platform returned success and the published Post record persisted | `platform`, `kind=post` |
| `review_replied` | Platform returned success and the replied Review state persisted | `platform`, `kind=review_reply` |

Frontend `page_view`, `button_click`, `chat_send`, `activation`, and integration
click events remain intent signals. They do not stand in for these completion
events. The client telemetry endpoint keeps its existing six-type allowlist and
drops `value`, so callers cannot forge server completions or identifiers.

Value rows may contain user and business UUIDs obtained from trusted server
state. Metadata is built from typed fields and contains only the bounded
`platform` and `kind` values. Source identities, content, review text, author
details, email addresses, credentials, tokens, and tool arguments are excluded.

Each completion derives a SHA-256 `server_dedupe_key` from its action and a
stable internal source identity. A partial unique database index and
`ON CONFLICT DO NOTHING` make repeated resume, direct-reply, and retry handling
idempotent. One cross-platform fan-out creates one event per successful platform
call because each tool-call identity is distinct.

Delivery to the durable table is bounded best-effort. Approval and value telemetry share an
eight-write semaphore, each write has a two-second deadline, and saturated or
failed writes are dropped without changing the business result. There is no
durable telemetry outbox, so a process crash before the asynchronous insert can
lose an event. The dedupe index provides at-most-once storage for attempted
redelivery; it does not guarantee eventual delivery.
