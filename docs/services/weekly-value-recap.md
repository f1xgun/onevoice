# Weekly value recap

The weekly value recap is a counts-only, in-app summary for the previous closed
Monday-to-Monday UTC week. It reports completed operations recorded by OneVoice;
it does not claim that an external platform was checked again.

The API counts `published_at` on published posts, `replied_at` on replied reviews
with a nonempty string `dispatch_approval_id`, and `completed_at` on successful
profile-operation tasks from a closed type/platform allowlist. It never reads
`created_at`, text, author data, prompts, or LLM output. Every active business gets
one PostgreSQL row per week, including zero and low-count rows. The read endpoint
returns only the exact prior week and suppresses totals below two with `204`.

`WEEKLY_VALUE_RECAP_ENABLED` is false by default. When enabled, a transaction
scoped PostgreSQL advisory lock admits one replica per pass. The pass has a fixed
deadline, each organization has a shorter timeout, and failures are isolated per
organization. `WEEKLY_VALUE_RECAP_POLL_INTERVAL` defaults to 24 hours.

The dashboard dismissal key contains user, organization, and week. Client events
use the closed `value_recap` actions `shown` and `dismissed`, are sent through the
authorized business telemetry route, and carry no recap counts or content.
