# Weekly owner brief delivery guards

`OwnerBriefService` uses the existing organization settings, schedule, model, and stable per-organization/week dispatch identity. Runtime delivery still requires the existing environment enablement, an eligible organization, and a configured private Telegram recipient.

API wiring supplies `OwnerBriefLock`, which holds a transaction-scoped PostgreSQL advisory lock on one connection throughout the pass. Another replica skips the overlapping pass. Missing lock wiring fails closed. The lock is released after success, callback failure, or connection teardown.

A pass has a five-minute deadline and runs at most four organizations concurrently. Each organization has a 45-second deadline covering statistics, composition, dispatch, and delivery stamping. Cancellation stops admission of additional work. Failures from individual organizations are collected after other admitted work completes, so the sweeper reports a partial failure instead of marking the whole pass successful.

Only active Telegram integrations with a positive numeric `telegram_user_id` are eligible. Channel/group IDs, handles, zero, malformed values, and overflow are rejected. The public channel `external_id` is never used as the private recipient. An empty reputation dataset is suppressed before generation, dispatch, stamping, or telemetry.

The existing aggregate-only content and opt-out behavior are preserved. This does not define plan-specific cadence or add review text to messages; those remain separate product decisions.

Verification includes existing weekly-idempotency and tenant-scope tests, empty-data and recipient rejection regressions, partial-failure behavior, and a real PostgreSQL test proving that a second connection cannot enter the pass and can acquire the lock after a failed first pass. Message delivery and model calls are faked in tests.
