`hasFirstSuccessfulAction` is true when any of the following evidence exists for the organization:

- A completed platform tool task (`agent_tasks.status = done`).
- An explicit successful turn outcome: a complete assistant message with `successful_outcome = true`, nonblank content, no error code, and no tool calls.
- A successful review action: `successful_action = true`, recorded for a nonblank generated reply draft or a confirmed dispatched nonblank reply. This marker survives draft cleanup. A legacy draft also counts when `draft_status = ready`, `draft_reply` is nonblank, and `draft_error` is absent or empty.

Ambiguous historical turns, failed or incomplete drafts, imported replies, and unconfirmed dispatches do not by themselves count. The backfill copies organization IDs only, never success outcomes.
