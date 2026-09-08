# Approval telemetry

Approval telemetry measures the owner-controlled path from a visible draft to
a recorded decision and a confirmed dispatch result. It covers posts and
review replies equally. It does not change tool floors, the HITL state machine,
or the requirement for an explicit owner decision.

## Events

All events are stored in `telemetry_events`. The client can write only
`event_type = approval`; `event_type = approval_server` is dropped by the
telemetry ingest service and can only be produced by server-side sinks.

| Producer | `event_type`      | `action`            | Meaning                                                                                                                            |
| -------- | ----------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Client   | `approval`        | `draft_shown`       | A ready post or review-reply draft was rendered.                                                                                   |
| Client   | `approval`        | `approval_shown`    | The owner could act on the rendered approval controls.                                                                             |
| Client   | `approval`        | `edit_saved`        | A changed chat field was accepted by the resolve endpoint.                                                                         |
| Server   | `approval_server` | `edit_saved`        | A changed, non-empty Reviews draft was persisted by the reply endpoint.                                                            |
| Server   | `approval_server` | `decision_recorded` | The server persisted the final per-call HITL verdict, or accepted a direct review-reply decision after its state and tenant gates. |
| Server   | `approval_server` | `send_result`       | An approved dispatch returned success or error; `not_dispatched` means the direct review path had no supported live dispatcher.    |

`decision_recorded.outcome` is `approve`, `edit`, or `reject`.
`edit_saved` has no outcome. Chat produces it on the client after resolve
success; Reviews produces it on the server after the first successful repository
operation that persists the final reply text and status. This can be
`UpdateReplyDispatched` after a successful platform dispatch or `UpdateReply`
after an error or no dispatch. A missing draft, unchanged reply, failure of every
applicable persistence operation, or retry does not produce it.
`send_result.outcome` is `success`, `error`, or `not_dispatched`. A retry of a
failed review reply emits another `send_result` and does not invent another
owner decision. Review autopublish emits neither event because it has no owner
approval. An expired, malformed, duplicate, cross-tenant, or otherwise refused
request emits no decision event. A valid owner rejection is a recorded
decision: an accepted `reject` verdict emits `decision_recorded` with
`outcome = reject`.

Chat `send_result` events are emitted only for a `tool_result` whose call ID is
present in the atomically claimed batch and whose persisted verdict is
`approve` or `edit`. The content kind comes from the persisted tool name, never
from the SSE frame. Duplicate and orphan results are ignored.

## Fields and content boundary

The approval schema is closed. Metadata contains only these fields:

| Field         |   Client |      Server | Values                                              |
| ------------- | -------: | ----------: | --------------------------------------------------- |
| `draft_id`    | required |    required | Lowercase SHA-256 hex                               |
| `approval_id` |   absent |    required | Lowercase SHA-256 hex                               |
| `batch_id`    |   absent |   chat only | Lowercase SHA-256 hex                               |
| `kind`        | required |    required | `post`, `review_reply`                              |
| `source`      | required |    required | `chat`, `reviews`                                   |
| `outcome`     |   absent | conditional | Closed values listed above; absent for `edit_saved` |

`correlation_id` equals `draft_id`, and `page` is the static `/chat` or
`/reviews`. Approval rows do not store `user_id`, `business_id`, or client
timestamps. The projection discards every other client field before
persistence, including caller-supplied page and correlation values.

Draft text, final publication or reply text, review text, author data, channel
identifiers, reject reasons, tool arguments, tool results, errors, and tool
names must never enter approval telemetry. Both client and server mappings use
closed tool-name lists. Unknown tools produce no approval event.

## Stable identities and joins

The hash input is always an opaque dispatch identity; content is never hashed.

- Chat drafts and dispatches use
  `SHA256(batch_id + "-" + tool_call_id)` for both `draft_id` and
  `approval_id`. This is the existing approval/dedupe identity documented as
  the Approval ID in `CONTEXT.md`.
- A direct Reviews-page draft uses
  `SHA256("review-reply-" + review_id)` as `draft_id`.
- A direct review dispatch uses the review's persisted
  `DispatchApprovalID` as the hash input: the stored telemetry field is
  `approval_id = SHA256(DispatchApprovalID)`. If the review originated in a
  chat approval, the hash input is the same
  `batch_id + "-" + tool_call_id` identity as the chat events. Legacy and
  Reviews-only rows use
  `"review-reply-" + review_id`, so `draft_id = approval_id`.
- Chat server events also carry `SHA256(batch_id)` as `batch_id`. This joins to
  the existing `hitl.approval_resolved` audit row by hashing
  `audit_logs.details.batch_id` with SHA-256 in the query. Audit storage remains
  unchanged.

For funnel analysis, join client and server telemetry on
`metadata->>'draft_id'`. Join a Reviews-page draft to the result of a dispatch
that originally came from chat through the review's server row: its `draft_id`
identifies the Reviews surface and its `approval_id` identifies the original
chat approval. Retry result rows preserve both values.

## Delivery limits

Client events use the existing in-memory five-second batch. Closing the browser,
logging out, a failed hash operation, or a failed request can lose events.

Server events are also best effort and never delay an approval or dispatch.
One call records all events from a multi-call decision as one database batch.
Writes run asynchronously with a two-second storage deadline and a process-wide
limit of eight concurrent writes. A saturated limit, process shutdown, timeout,
or database error drops the telemetry batch. There is no durable queue, retry,
or ordering guarantee. These limits protect the API latency target; telemetry
must not become part of the approval or publication transaction.

The impressions are render impressions, not viewport-observer impressions.
They are deduplicated while a draft component remains mounted, including React
Strict Mode effect replay, and can be counted again after an unmount/remount.

## Tool mapping

The client and server tests pin the same publication set:

- Posts: `telegram__send_channel_post`, `telegram__send_channel_photo`,
  `vk__publish_post`, `vk__post_photo`, `vk__schedule_post`,
  `yandex_business__create_post`.
- Review replies: `telegram__reply_to_comment`, `vk__reply_comment`,
  `yandex_business__reply_review`, `google_business__reply_review`.

Changes to the orchestrator publication tools must update both mappings and
their parity tests in the same change.
