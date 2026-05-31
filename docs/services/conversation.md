# ConversationService

Owns conversation operations that compose more than one repository write or read into a single domain transition. Pure CRUD reads/writes stay on the handler-to-repo path until they grow shared logic.

## Public API

- `MoveToProject` — replaces an inline four-op sequence in `ConversationHandler.MoveConversation`. Moves a conversation to a different project (or to no project when `projectID` is nil/empty) and returns the post-move conversation.
- `OpenChat` — replaces an inline four-op sequence + soft-error + projection in `ConversationHandler.ListMessages`, returning a fully-projected `*ChatView` ready for JSON encoding by the handler.

## Construction contract

`NewConversationService` requires every repository dependency (`convRepo`, `messageRepo`, `projectRepo`, `pendingRepo`). A nil arg is a programmer error caught at boot, not a fallback the service silently accommodates. Construction returns an error rather than panicking so the wiring layer can surface a clear startup failure.

## Wire-shape projections (`ChatView`, `PendingApprovalSummary`, `ApprovalCallSummary`)

`ChatView` is the API contract returned by `OpenChat` — a fully-projected view of a conversation's messages + active approval batches, ready for JSON encoding by the handler. The JSON tags travel with the value object because the projection (camelCase `pendingApprovals`, stable empty `[]`) IS the contract — keeping it adjacent to `OpenChat` concentrates the "shape of `GET /messages`" decisions in one place.

`PendingApprovals` is ALWAYS serialized as a non-nil slice (even when empty) so frontend code can iterate unconditionally; `OpenChat` enforces this regardless of whether the pending lookup soft-errored.

`PendingApprovalSummary` is the per-batch projection emitted by `OpenChat`. Each field name matches the JSON contract the frontend consumes to render the approval card on page reload. `EditableFields` on each call is intentionally left empty in this response: the frontend already has the live tool registry via the `['tools']` React Query (`GET /api/v1/tools`), which is the single source of truth for per-tool editable-field whitelists. The field is still emitted as `[]` (not omitted) so the JSON schema stays stable for downstream consumers.

`ApprovalCallSummary` is the api → frontend (camelCase) projection of an approval batch element. Distinct from `pkg/sse.ApprovalCall`, which is the orchestrator → api wire (snake_case) shape: the two consumers have different naming conventions and slightly different field sets (no `Floor` here because the FE has its own tools cache for that), so two types serve two contracts.

## Pagination

`defaultMessageListLimit` (200) caps the number of messages `OpenChat` returns. The frontend chat history view renders the latest N; older entries require explicit pagination, which is not yet exposed via `OpenChat`.

## Business rules

### `MoveToProject` lifecycle

Owns the full transition end-to-end:

1. Fetch the conversation. Missing → `ErrConversationNotFound`.
2. Enforce ownership — the requester must be the conversation's user. Cross-user attempts surface as `ErrForbidden` so the handler returns a uniform 403 without leaking existence.
3. Resolve the destination display name — for an explicit `projectID`, load the project and verify it belongs to `businessID`; cross-tenant access surfaces as `ErrProjectNotFound`, mirroring `ProjectService.GetByID`. For nil/empty `projectID`, use the localized "no project" label.
4. Persist the `project_id` assignment via the repo's atomic single-field update.
5. Append a localized system note to the conversation history. Best-effort — the move already landed; a failed note is logged but does not fail the request.
6. Re-fetch and return the post-move conversation.

Locale for the destination label and system note comes from `ctx` via `i18n.Tr` — middleware injects it upstream.

### `OpenChat` lifecycle

Returns the assembled view rendered by `GET /messages` — ownership-checked messages list + projected pending-approval batches — in a single call. Composes four repo reads with one soft-error policy (pending lookup failures degrade gracefully to an empty array) and emits the wire-shape projection so the handler is a pure encoding step.

1. Fetch the conversation. Missing → `ErrConversationNotFound`.
2. Enforce ownership — the requester must be the conversation's user. Cross-user attempts surface as `ErrForbidden`.
3. Load the latest messages (capped at `defaultMessageListLimit`).
4. Load active approval batches. Failure is non-fatal: logged and surfaced as an empty `PendingApprovals` slice. Rationale: the messages list is still useful for chat history; failing the entire request because of an approval-card hydration miss would be more surprising than a missing card.
5. Project each batch into the camelCase wire shape and assemble the final `ChatView`.

## Error semantics

### `MoveToProject`

- `ErrInvalidProjectID` — `projectID` is non-empty but does not parse as a UUID. Distinct from `ErrProjectNotFound` so the handler can map malformed input to 400 and missing-project to 404 without duplicating the parse check.
- `domain.ErrConversationNotFound` — conversation does not exist.
- `domain.ErrForbidden` — conversation exists, caller is not the owner.
- `domain.ErrProjectNotFound` — `projectID` points to a missing or cross-tenant project. Cross-tenant access surfaces as not-found rather than forbidden to avoid existence enumeration.
- other — persistence errors propagated verbatim.

System-note creation is best-effort: the move itself already landed. Failing the request would leave the conversation in its new project without the audit note and offer no undo path. The move is kept atomic from the caller's POV; a missing note is observable but recoverable.

### `OpenChat`

- `domain.ErrConversationNotFound` — conversation does not exist.
- `domain.ErrForbidden` — conversation exists, caller is not the owner.
- other — persistence errors propagated verbatim (only the messages lookup blocks the request; pending lookups soft-error).

## Cross-references

- [docs/architecture.md](../architecture.md)
- [docs/api-design.md](../api-design.md)
- `pkg/sse.ApprovalCall` — sibling orchestrator → api wire type with different naming conventions.
