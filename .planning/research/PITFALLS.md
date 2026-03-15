# Pitfalls Research

OneVoice — common mistakes and how to prevent them across VK API, Yandex RPA, JWT/auth, monitoring, and multi-service testing.

---

## VK API Integration Pitfalls

### 1. Rate Limiting Misconfiguration

**What goes wrong:** VK API enforces per-user and per-app rate limits (approximately 3 requests/second per user token, lower for batch calls). The VK SDK (`SevereCloud/vksdk/v3`) does not automatically throttle or retry on 429. The current `vk__create_wall_post` implementation makes no attempt to track or back off from rate limits. Under moderate load (e.g., LLM making multiple tool calls in a single session), consecutive wall posts trigger `Error 9: Flood control enabled`. The orchestrator interprets this as a permanent tool failure and reports it to the LLM, which may try the same call again — doubling the flood.

**Warning signs:**
- `VK API Error 9` in logs after rapid successive tool calls
- LLM retrying a tool call that already hit rate limits
- Multiple business users sharing one VK app getting collectively throttled

**Prevention strategy:**
1. Add per-businessID sliding window rate limiter for VK calls in the agent (Redis-backed, reuse existing pattern from `pkg/llm/ratelimit.go`).
2. Parse VK API error codes (`err.Code == 9`) and return a `NonRetryableError` with a backoff hint. The orchestrator should surface "VK is rate-limiting; wait 30s" to the user rather than silently failing.
3. VK's `execute` batch method bundles up to 25 operations in one API call — evaluate for bulk operations.

**Phase:** Address in the VK agent completion phase (current active work).

---

### 2. Token Expiry and Revocation

**What goes wrong:** VK access tokens with `offline` scope do not technically expire, but they are revoked when: the user changes their VK password, the app is deauthorized in VK settings, or VK detects abuse. The current `oauth.go` refresh logic checks `token_expires_at` but does not handle the revoked-token case (`Error 5: User authorization failed`). A revoked token will cause every tool call to fail permanently, but the system will keep trying (and logging the same error) until a human notices.

**Warning signs:**
- Steady stream of `Error 5` or `Error 15` (Access denied) in VK agent logs
- Tool calls returning error but orchestrator not surfacing it to user clearly
- Integration shows `active` status in database despite all calls failing

**Prevention strategy:**
1. In the VK agent, detect `Error 5` and `Error 15` and publish a status update to the API service (via internal endpoint) to mark the integration as `expired` or `revoked`. The integration list page should surface this state.
2. Add a VK token validation check on agent startup: call `users.get` with the stored token; if it fails with `Error 5`, log a critical warning and mark integration inactive.
3. The orchestrator should include integration status in the available tools list and exclude tools for expired integrations (already exists via `activeIntegrations` filter — ensure the filter is kept up-to-date).

**Phase:** VK agent completion + security hardening phase.

---

### 3. API Version Drift

**What goes wrong:** The codebase pins `v5.131` of the VK API (via `vksdk/v3`). VK periodically deprecates older API versions. Fields in responses may change format or disappear without a 400 error — VK often returns empty strings or zero values for deprecated fields. Code that assumes `post.CopyHistory[0].Text` exists will panic silently on zero-value access when the field format changes.

**Warning signs:**
- VK tool returns empty results without any error
- VK changelog announces deprecation of fields used in the integration
- Test accounts return different data shapes than production accounts

**Prevention strategy:**
1. Set `vksdk` to use the latest stable API version supported by the SDK. Check sdk release notes before upgrading.
2. Use defensive access patterns: never index into slices from VK responses without length checks. All type assertions from `interface{}` should use the comma-ok form.
3. Add a scheduled canary test that calls `wall.get` on a test community and validates response shape. Run weekly in CI.

**Phase:** VK agent completion, with ongoing maintenance.

---

### 4. Community vs. User Wall Permissions

**What goes wrong:** `wall.post` behaves differently depending on whether `owner_id` is negative (community) or positive (user). Posting to a community requires that the OAuth token belongs to a community manager/admin. If the business owner authorized via OAuth as a regular member, `wall.post` returns `Error 7: Permission denied` or silently posts to the user's wall instead of the community wall. The LLM may not distinguish between these two cases and the user sees no post where they expected one.

**Warning signs:**
- `owner_id` is negative but the post appears on a personal wall
- `Error 7` or `Error 15` on community posts
- LLM tool result shows `post_id > 0` but community has no new post

**Prevention strategy:**
1. At integration setup time (OAuth callback), call `groups.getById` with the connected token to verify the user has `editor` or `administrator` role for the target community. Store the verified community ID as `external_id` in the integrations table.
2. In the tool definition for `vk__create_wall_post`, require `owner_id` to be a negative number (validate in the agent handler before the API call).
3. Distinguish `Error 7` as a permanent configuration error (not retryable), surfaced to the user as "VK integration requires community admin permissions."

**Phase:** VK agent completion.

---

## Yandex.Business RPA Pitfalls

### 1. Memory Leaks from Browser Instance Accumulation

**What goes wrong:** The current `browser.go` uses a single Playwright browser instance reused across operations via `withPage`. If a Playwright operation hangs (e.g., page load timeout waiting for a slow Yandex CDN), the page is not released back to the pool properly. Over time, multiple hung pages accumulate in the browser process. Each page consumes roughly 50–150MB of browser memory. After enough operations, the agent pod OOM-kills, losing any in-progress tool calls silently.

**Warning signs:**
- Agent pod memory grows monotonically; never plateaus
- RSS memory of `chromium` subprocess climbs over hours
- `withPage` calls start timing out without network errors

**Prevention strategy:**
1. Enforce a hard timeout on every `withPage` call (currently missing). Set `page.SetDefaultTimeout(30000)` at page creation time, not per-action.
2. Use `defer page.Close()` inside `withPage` to guarantee page cleanup even on panic.
3. Add a memory watchdog: if the Chromium subprocess RSS exceeds a configurable threshold (e.g., 1GB), restart it gracefully before the next tool call. Log the restart event.
4. Use Playwright's `BrowserContext` isolation per operation (create and destroy context per tool call) rather than sharing one browser context. Slightly more overhead but eliminates state leaks between operations.

**Phase:** Yandex agent implementation phase.

---

### 2. Zombie Browser Processes After Agent Crash

**What goes wrong:** If the Go agent process panics or is killed by SIGKILL (OOM), the Playwright-spawned Chromium subprocess is not cleaned up — it becomes a zombie or orphan process. On the next agent restart, a new Chromium process is launched. After several crash-restart cycles, multiple Chromium processes run simultaneously, consuming CPU and memory. The OS may eventually block new process creation.

**Warning signs:**
- `ps aux | grep chromium` shows multiple processes after several agent restarts
- Disk full errors in `/tmp/playwright-*` temp directories
- Agent fails to launch Playwright with `executable not found` after file descriptor exhaustion

**Prevention strategy:**
1. On agent startup, scan for and kill orphaned Chromium processes before launching a new one. Use `pkill -f chromium` before `playwright.Run()` (acceptable in containerized environments where the agent is the only Chromium user).
2. Implement graceful shutdown: register SIGTERM handler that calls `browser.Close()` before exit. The current `main.go` exit path needs this.
3. Run the agent in a container with `--init` (tini) so orphan child processes are properly reaped when the agent exits.
4. Set a maximum restart count in the container orchestrator (Kubernetes `restartPolicy: OnFailure` with backoff).

**Phase:** Yandex agent implementation + deployment hardening.

---

### 3. Selector Drift from Yandex DOM Changes

**What goes wrong:** Yandex.Business is a closed platform with no stability guarantees on its DOM structure. A CSS class rename or React component refactor silently breaks all selectors. The current stub implementations have no selectors yet, but once written they will need active maintenance. The most common failure mode is: selectors match the wrong element (e.g., a button that is still present but now has different behavior), the action "succeeds" (no error thrown), but the wrong data is submitted.

**Warning signs:**
- Tool reports success but no change is visible in Yandex.Business UI
- `page.Locator(selector).Click()` completes but the expected form doesn't open
- Playwright screenshots show a different page state than expected

**Prevention strategy:**
1. Use Playwright's `getByRole`, `getByText`, and `getByLabel` selectors where possible — they are more resilient to class renames than CSS selectors.
2. After every action that should produce a change (e.g., submit hours form), add an explicit assertion: wait for a success toast or confirmation element. Treat the absence of confirmation as a failure, not a success.
3. Capture a screenshot at the end of every tool call and attach it to the tool response as a debug artifact. Log to structured storage (even local disk) for post-mortem analysis.
4. Schedule a weekly smoke test that opens the Yandex.Business dashboard in headful mode (locally) and checks that key selectors still resolve. Alert if they fail.

**Phase:** Yandex agent implementation, with ongoing maintenance.

---

### 4. Anti-Bot Detection Triggering CAPTCHA

**What goes wrong:** Yandex uses behavioral fingerprinting and CAPTCHA to detect automation. The `humanDelay` helper adds 500–1500ms random delays, but this is insufficient by itself. Patterns that trigger detection: perfect mouse trajectories (Playwright clicks at exact element centers), missing mouse movement between actions, identical timing distributions across sessions, and missing browser entropy (fonts, canvas fingerprint, timezone mismatches).

**Warning signs:**
- Yandex.Business redirects to a CAPTCHA page mid-session
- `canary check` fails with unexpected redirect to `/showcaptcha`
- Sessions expire much faster than expected (minutes instead of hours)

**Prevention strategy:**
1. Use `page.Mouse.Move()` with intermediate waypoints before clicks to simulate realistic mouse movement.
2. Launch Playwright with a realistic user agent and full browser profile (timezone, language, screen resolution matching a real business user's setup). Avoid headless detection markers: set `--disable-blink-features=AutomationControlled`.
3. Maintain session cookies from an actual real logged-in session and refresh them regularly (weekly or before each run). Do not use programmatic login flows — they are the highest-risk detection vector.
4. Implement CAPTCHA detection in the canary check: if the page URL contains `showcaptcha` or the title contains "Я не робот", return a specific error type (`ErrCaptchaDetected`) so the user is notified and manual intervention can refresh the session.

**Phase:** Yandex agent implementation.

---

### 5. Stale Cookie Handling and Session Recovery

**What goes wrong:** Already identified in CONCERNS.md — `setCookies()` silently discards invalid cookie fields. Beyond that, even valid cookies expire. The current `withRetry` will retry a failed action up to 3 times, but if the session is expired, all 3 attempts will fail the same way. This wastes 3 × exponential backoff time (7+ seconds) before returning an error to the user. Worse, the canary check navigates to the dashboard but may not detect a soft expiry (Yandex sometimes shows a partial dashboard to expired sessions before redirecting to login).

**Warning signs:**
- All tool calls fail with page redirect to login page
- Retry attempts all produce identical errors
- `YANDEX_COOKIES_JSON` was set more than 30 days ago (Yandex session cookies typically last 30–90 days)

**Prevention strategy:**
1. Implement a robust canary check: after navigation, assert that a specific authenticated element is present (e.g., the business name heading). Use `page.Locator(".business-name").IsVisible()` with a short timeout (5s). If not visible, return `ErrSessionExpired` immediately without retrying.
2. Log cookie expiry times at agent startup so operators can predict and proactively refresh sessions. Alert 7 days before the earliest cookie expiry.
3. In `withRetry`, check if the error is `ErrSessionExpired` and skip all retry attempts — session expiry is not a transient error.
4. Consider building a cookie refresh helper: a separate script that logs in interactively (with human CAPTCHA solving) and outputs fresh cookies to be passed as the new env variable value.

**Phase:** Yandex agent implementation + security hardening.

---

## JWT/Auth Security Pitfalls

### 1. Algorithm Confusion (alg:none and RS256/HS256 Confusion)

**What goes wrong:** The codebase uses `jwt.Parse()` with `jwt.MapClaims`. If the parser does not explicitly validate the `alg` header field, a crafted JWT with `"alg":"none"` and no signature passes validation in some JWT library versions. Additionally, if the application ever adds RSA-based tokens (e.g., for a third-party integration), an attacker can craft a JWT signed with the RSA public key using HS256 — if the verifier uses the public key as the HMAC secret, the signature validates.

**Warning signs:**
- JWT validation does not explicitly specify expected algorithms
- `jwt.Parse()` is called without a `jwt.WithValidMethods()` option
- Different token issuers use different algorithms with the same validation code path

**Prevention strategy:**
1. Always use `jwt.WithValidMethods([]string{"HS256"})` when parsing. The `golang-jwt/jwt/v5` library supports this via `ParseOptions`.
2. Use typed claims (`CustomClaims` struct with `jwt.RegisteredClaims` embedded) and `jwt.ParseWithClaims()` — already recommended in CONCERNS.md. Typed claims also prevent type confusion attacks where `user_id` is sent as an integer.
3. Add a test that attempts to validate a `alg:none` token and asserts it returns an error.

**Phase:** Security hardening phase (active).

---

### 2. Refresh Token Replay and Revocation Races

**What goes wrong:** The current refresh token implementation stores a hash in PostgreSQL with a 7-day TTL. The refresh endpoint does a lookup + delete + issue (non-atomic). Under race conditions with concurrent requests (e.g., a mobile client with reconnect logic sending two refresh requests simultaneously), both requests may read the old token before either deletes it. Both receive new tokens. One of those new tokens is then "orphaned" — the user has two valid sessions, neither aware of the other. If an attacker also had the original refresh token (e.g., stolen from localStorage), the attacker can also exchange it simultaneously.

**Warning signs:**
- User reports being logged in on multiple devices after they expected single-session behavior
- Two valid refresh tokens visible in the `refresh_tokens` table for the same user
- Refresh endpoint occasionally returns 401 for a token that was valid a moment ago (the concurrent delete race)

**Prevention strategy:**
1. Make the refresh operation atomic using a PostgreSQL `DELETE ... RETURNING` query: delete the old token and return it in one statement. If 0 rows returned, the token was already used — return 401. This eliminates the read-then-delete race.
2. Implement refresh token rotation: when a refresh token is used, issue a new one and revoke the old. If the old token is presented again after rotation, treat it as a token theft signal and revoke all tokens for that user.
3. Track the `family` of refresh tokens (a UUID shared across all rotations from the original login). If a revoked family member is presented, revoke all tokens in the family.

**Phase:** Security hardening phase (active).

---

### 3. JWT Secret Rotation Without Downtime

**What goes wrong:** The `JWT_SECRET` is a single static value. If it must be rotated (compromise, scheduled rotation), all currently valid access tokens immediately become invalid. Every active user is logged out simultaneously. This creates a support incident and is a disincentive to rotate secrets regularly.

**Warning signs:**
- JWT_SECRET has never been rotated since initial deployment
- No rotation procedure documented
- Secret rotation == mass logout

**Prevention strategy:**
1. Support multiple valid JWT secrets simultaneously: primary + previous. Validation tries primary first, then falls back to previous. New tokens always use primary. After the access token TTL (1 hour), all tokens are signed by the new primary.
2. Store the key identifier in the JWT `kid` header field. The validator selects the correct key by `kid`. This also works for RS256/ES256 with a JWKS endpoint.
3. Define a rotation procedure in the runbook: rotate primary → old primary becomes secondary → after 2 hours (2× access token TTL) remove secondary.

**Phase:** Security hardening phase.

---

### 4. Refresh Token in localStorage (Already in CONCERNS.md — Extended Analysis)

**What goes wrong:** XSS is the primary attack vector, but the threat surface is wider than just inline scripts. In Next.js 14, third-party analytics scripts (if ever added), compromised npm packages (supply chain), and browser extensions with broad permissions can all read `localStorage`. The refresh token has a 7-day window — far longer than an access token — giving attackers an extended window for impersonation.

**Warning signs:**
- Application adding any third-party JavaScript (analytics, chat widgets, A/B testing)
- `localStorage` accessed from any code path other than the auth library
- CSP headers missing or set to `unsafe-inline`

**Prevention strategy:**
1. Move refresh token to an `HttpOnly; Secure; SameSite=Strict` cookie. This requires API changes to set/clear the cookie on login/logout and frontend changes to remove localStorage access. The API already sets cookies in some paths — extend to refresh token.
2. Add `Content-Security-Policy: script-src 'self'` header to block third-party script execution. Enforce via Next.js `headers()` config.
3. For the diploma demo, at minimum add `SameSite=Strict` to all cookies and document the localStorage risk in the security section of the thesis.

**Phase:** Security hardening phase (active).

---

### 5. Missing JWT Audience and Issuer Validation

**What goes wrong:** The current JWT validation checks signature and expiry but not `iss` (issuer) or `aud` (audience) claims. If another service in the ecosystem (or a future integration) also issues JWTs signed with a similar HS256 key, a token from that service could be replayed against the OneVoice API. More concretely: the orchestrator service and the API service both exist — if either ever issues JWTs, they must not be interchangeable.

**Warning signs:**
- JWT `iss` claim is not set at token creation time
- JWT validation does not assert `iss == "onevoice-api"`
- Multiple services using JWT without namespace separation

**Prevention strategy:**
1. Set `Issuer: "onevoice-api"` in `RegisteredClaims` at token creation.
2. Set `Audience: jwt.ClaimStrings{"onevoice-frontend"}` to scope tokens to their intended consumer.
3. Add `jwt.WithIssuers("onevoice-api")` and `jwt.WithAudiences("onevoice-frontend")` to all `ParseWithClaims` calls.

**Phase:** Security hardening phase.

---

## Monitoring Anti-Patterns

### 1. Alert Fatigue from Low-Threshold Rate Alerts

**What goes wrong:** The first instinct when adding Prometheus metrics is to alert on every non-zero error count. In a multi-agent system with external API calls (VK, Telegram, Yandex), transient errors are expected and normal. Alerting on "any 5xx from any service" produces constant noise from VK rate limits, Telegram API hiccups, and network blips. On-call engineers begin ignoring alerts. The one real incident — a database connection pool exhaustion — is lost in the noise.

**Warning signs:**
- Alerts fire daily or more frequently
- Engineers acknowledge alerts without investigating
- Alert history shows long periods between "resolved" and next "firing" with no human action
- No documented alert runbook exists

**Prevention strategy:**
1. Alert on rates and trends, not raw counts: `rate(http_requests_total{status=~"5.."}[5m]) > 0.05` (5% error rate over 5 minutes), not `http_requests_total{status="500"} > 0`.
2. Separate alerts by severity: page for database down or NATS disconnected; ticket/Slack for elevated error rates; dashboard-only for individual transient errors.
3. Every alert must have a linked runbook with: "what this means," "likely causes," "immediate actions." If you can't write the runbook, the alert is not ready.
4. Use Prometheus `for: 5m` on all alerts to require the condition to persist before firing. One spike should not wake anyone up.

**Phase:** Monitoring phase (active).

---

### 2. Cardinality Explosion in Metric Labels

**What goes wrong:** Adding high-cardinality labels to metrics is the most common Prometheus mistake. In OneVoice, the tempting but catastrophic label choices are: `business_id` (every user gets a unique ID — 10,000 businesses = 10,000 time series per metric), `conversation_id`, `tool_name` with arbitrary user-supplied values, or `user_id`. Prometheus stores all time series in memory. At high cardinality, the Prometheus server OOM-kills long before any business value is extracted.

**Warning signs:**
- Prometheus memory grows continuously as user count grows
- `prometheus_tsdb_head_series` metric climbs steadily
- Query performance degrades: `rate()` queries timeout
- Label value sets include UUIDs, email addresses, or raw text

**Prevention strategy:**
1. Only use low-cardinality labels: `service`, `platform` (telegram/vk/yandex_business), `tool_name` (known set of ~10 tools), `status` (success/error), `http_method`, `endpoint` (route pattern not URL).
2. Never use `business_id`, `user_id`, `conversation_id`, or any free-form string as a label.
3. For per-business metrics, use application-level aggregation (e.g., a daily job that writes aggregate stats to PostgreSQL) rather than Prometheus.
4. Set `metric_relabel_configs` in Prometheus to drop or hash high-cardinality labels if they sneak in via middleware.

**Phase:** Monitoring phase.

---

### 3. Missing Distributed Trace Correlation

**What goes wrong:** The system has structured logging (`slog`) but no request correlation across service boundaries. A chat request flows: Frontend → API (`chat_proxy.go`) → Orchestrator → NATS → Telegram Agent → Telegram API. When a tool call fails, logs exist in 3+ services. Without a shared `request_id` or `trace_id` threading through all logs, diagnosing "why did this specific user's post fail at 14:32?" requires manually correlating timestamps across multiple log files — which fails when clocks drift or when logs are interleaved from concurrent requests.

**Warning signs:**
- Debugging a production issue requires SSH into multiple containers and manual timestamp correlation
- NATS messages have no tracing metadata
- SSE events have no correlation to the originating HTTP request

**Prevention strategy:**
1. Generate a `trace_id` (UUID) at the API layer for every chat request. Propagate it in all downstream calls: as an HTTP header (`X-Trace-ID`) to the orchestrator, and as a field in the A2A `TaskRequest` struct.
2. Log the `trace_id` in every log line related to that request (use `slog.With("trace_id", traceID)` to create a child logger).
3. For a lightweight implementation without OpenTelemetry: add `request_id` field to `TaskRequest` (already has a stub) and log it in every agent handler.
4. If adding OpenTelemetry: instrument the NATS transport in `pkg/a2a/nats_transport.go` to propagate W3C trace context headers in NATS message headers.

**Phase:** Monitoring phase + ongoing.

---

### 4. Health Checks That Pass When Service Is Degraded

**What goes wrong:** A simple `/health` endpoint that returns 200 if the HTTP server is running misses all the interesting failures. In OneVoice, a service can be "up" (HTTP server running) but effectively dead: NATS connection dropped (no tool calls will work), PostgreSQL pool exhausted (no DB operations), or MongoDB replica set primary election (writes fail). A load balancer that routes traffic to a degraded service amplifies the problem.

**Warning signs:**
- Health check says UP but tool calls return errors
- NATS disconnect is not detected for minutes (until the next tool call fails)
- Database pool exhaustion not surfaced until requests start failing

**Prevention strategy:**
1. Implement a deep health check that tests each dependency: PostgreSQL (`db.Ping()`), MongoDB (`client.Ping()`), Redis (`client.Ping()`), NATS (check `nc.IsConnected()`). Return 503 if any required dependency is unhealthy.
2. Separate liveness (is the process alive?) from readiness (can it handle traffic?) probes for Kubernetes. Liveness: simple 200. Readiness: deep check.
3. Cache health check results for 5 seconds to avoid `Ping()` on every Kubernetes probe (probes run every 10s by default).
4. Return structured JSON from the health endpoint: `{"status":"degraded","checks":{"postgres":"ok","nats":"error: connection refused"}}`. This makes debugging much faster.

**Phase:** Monitoring phase (active).

---

### 5. No Visibility Into NATS Queue Depth or Timeout Rate

**What goes wrong:** NATS request/reply is the nervous system of the multi-agent system. If agents are slow, NATS queues build up. If an agent is down, requests time out silently (after the 30s timeout). Without metrics on NATS behavior, the first sign of an agent being overwhelmed is user complaints. By the time a human investigates, the queue has grown further.

**Warning signs:**
- Users report slow tool execution without any error
- Agent logs show processing time > 20s for routine operations
- No metrics exist for NATS message counts, timeouts, or latency

**Prevention strategy:**
1. Instrument the NATS executor (`natsexec/executor.go`) to record: request latency (histogram), timeout count (counter by `tool_name`), and error count.
2. Record agent-side processing time in the `TaskResponse` and log it in the orchestrator when receiving the reply.
3. Alert on NATS timeout rate exceeding 1% over 10 minutes — this indicates an agent is struggling.
4. Add a NATS-specific metric: subscribe count per subject (using NATS server's `/varz` HTTP endpoint) to detect agent disconnects.

**Phase:** Monitoring phase.

---

## Testing Gaps That Cause Incidents

### 1. No Chaos/Failure Tests for NATS Disconnects

**What goes wrong:** The orchestrator's NATS executor has a 30-second timeout, but what happens when NATS is unavailable (restart, network partition)? The current code attempts the request and waits the full timeout. If NATS is down during a chat session with multiple tool calls, each tool call adds 30 seconds before failing. A 5-tool-call session hangs for 2.5 minutes. This is a production incident waiting to happen — and it has never been tested.

**Warning signs:**
- No test for "NATS unavailable" code path
- Orchestrator code does not check `nc.IsConnected()` before publishing
- Agent tests only cover the happy path (request received, response sent)

**Prevention strategy:**
1. Add an integration test that starts the orchestrator with a NATS connection, sends a chat request that triggers a tool call, then kills NATS mid-request. Assert that the error is returned to the frontend within a reasonable time (not 30s per tool call).
2. Add a check in `NATSExecutor.Execute()`: if `nc.IsConnected()` is false, return an error immediately rather than waiting for timeout.
3. Implement circuit breaker per agent: after 3 consecutive NATS timeouts for a given agent ID, fail fast for the next 60 seconds rather than waiting the full timeout each time.

**Phase:** Testing/reliability phase.

---

### 2. Missing Tests for SSE Stream Interruption

**What goes wrong:** Already identified in CONCERNS.md — no tests for mid-stream errors. The deeper issue: the `chat_proxy.go` accumulates tool calls in memory and saves them to MongoDB only on stream completion (`done` event). If the client disconnects mid-stream (mobile network switch, browser tab closed), the tool calls and results are never persisted. The conversation shows the user message with no reply. On reconnect, the LLM lacks context of what tools were already called, potentially repeating actions (double-posting to Telegram).

**Warning signs:**
- No test for `context.Done()` during SSE streaming
- Tool call persistence relies on receiving the `done` event
- No idempotency checks on agent tool execution

**Prevention strategy:**
1. Decouple tool call persistence from stream completion: save each `tool_call` and `tool_result` event to MongoDB as it arrives, not only on `done`. Use the SSE event's `tool_call_id` as the document key.
2. Add a test that simulates client disconnect (cancel request context) after receiving 2 tool_call events but before `done`. Assert that the 2 tool calls are persisted in MongoDB.
3. For agent idempotency: add a `task_id` (already exists in `TaskRequest`) to Telegram/VK operations. Check if a post with that `task_id` was already submitted before making the API call. Store the mapping in Redis (TTL: 1 hour).

**Phase:** Testing/reliability phase.

---

### 3. Integration Tests Absent for Platform Agents

**What goes wrong:** VK and Yandex.Business agents have never been tested against real APIs. The agent code compiles, but the only "test" is whether it runs. VK's `wall.post` API response includes nested objects that the current handler may not parse correctly. The Playwright selectors in Yandex tools (once written) will not be validated until a human opens the browser and watches. Production is the first integration test.

**Warning signs:**
- Agent tests directory is empty or contains only compilation checks
- No VK sandbox/test community set up for automated testing
- Yandex.Business agent tests depend entirely on mocked Playwright interfaces

**Prevention strategy:**
1. Set up a VK test community with a long-lived admin token (use offline scope). Add integration tests to the CI pipeline that post to and delete from this community using the real VK API. Mark these tests with a build tag (`//go:build integration`) and run them in a scheduled job, not on every PR.
2. For Yandex RPA: record a Playwright test session against a test Yandex.Business account. Use Playwright's code generation (`playwright codegen`) to capture selectors. Store the test session as a smoke test that runs weekly.
3. Use `httptest` server to mock VK API responses for unit tests, covering error codes 5, 7, 9, 15, and rate limit responses. This does not require real credentials.

**Phase:** Testing/reliability phase + VK agent completion.

---

### 4. Race Condition Tests Missing for Concurrent Chat Sessions

**What goes wrong:** The orchestrator handles each chat request in a request goroutine. If two requests arrive for the same `conversationID` simultaneously (e.g., user rapidly submits two messages), both goroutines read the same conversation history from MongoDB, run LLM inference in parallel, and both try to save tool results. MongoDB document-level locking prevents a data race, but both LLM calls may produce conflicting tool calls (e.g., two Telegram posts instead of one). The second LLM call's context is stale (missing the results from the first call's tool execution).

**Warning signs:**
- No test for concurrent requests to the same conversation
- `go test -race ./...` not run in CI (or run but with many suppressed races)
- Conversation history is loaded once at the start of the agent loop, not re-fetched between iterations

**Prevention strategy:**
1. Add a per-`conversationID` mutex in the orchestrator handler to serialize requests to the same conversation. Redis-based distributed lock (using `SETNX`) is needed if the orchestrator is horizontally scaled.
2. Add a test that fires two concurrent requests to the same conversation endpoint and asserts only one LLM call is processed (the second waits or returns 429).
3. Run `go test -race ./...` on every CI run and fix all detected races. Do not suppress warnings.

**Phase:** Testing/reliability phase.

---

### 5. Test Coverage Metrics Mislead (High Line Coverage, Zero Behavior Coverage)

**What goes wrong:** The codebase has repository tests that are `assert.True(t, true)` placeholders. These count toward line coverage metrics. A 60% line coverage number that includes placeholder tests is meaningless. Meanwhile, the most critical paths — token encryption/decryption, JWT validation edge cases, NATS timeout handling — may have zero actual behavior tests despite being marked as "covered."

**Warning signs:**
- Repository test files exist but contain no assertions
- Coverage report looks acceptable but critical code paths (crypto, JWT validation, error handling) are not in coverage
- Coverage is measured per file, not per branch

**Prevention strategy:**
1. Replace placeholder tests with real behavior tests using `miniredis` for Redis, `pgxmock` for PostgreSQL, and `mongotest` for MongoDB. Start with the most critical: token encryption, refresh token lifecycle, integration token lookup.
2. Measure branch coverage, not just line coverage. A function with an `if err != nil` branch needs two test cases: one where the error occurs and one where it does not. Use `go test -covermode=atomic` with `cover -func` to identify untested branches.
3. Add a CI quality gate: PRs that reduce coverage on critical packages (`pkg/crypto`, `services/api/internal/middleware`) below a threshold are blocked. Do not apply this gate to stub packages.

**Phase:** Testing/reliability phase.

---

## Prevention Strategies

### Categorized by When to Apply

**Before writing agent code (design phase):**
- Define error taxonomy: transient (retry), permanent (user-action needed), rate-limited (backoff). Apply consistently across all agents and the orchestrator retry logic.
- Design idempotency into all tool calls using `task_id`. Store task results with the ID before making external API calls to prevent double-execution on retry.
- Define metric label vocabulary upfront: agree on `service`, `platform`, `tool_name`, `status` as the only metric dimensions. Reject PRs that add high-cardinality labels.

**During VK agent implementation:**
- Handle VK error codes 5, 7, 9, 15 with distinct error types before shipping.
- Validate community admin permissions at OAuth time, not at first post time.
- Write VK integration tests against a real test community before marking the agent as complete.

**During Yandex agent implementation:**
- Build the cookie expiry alert and canary check before writing any business logic selectors.
- After every selector, add an explicit post-action assertion.
- Test memory behavior: run 10 sequential tool calls and measure Chromium RSS before and after.

**During security hardening:**
- Move refresh token to HttpOnly cookie first — this unblocks all other security improvements.
- Implement atomic refresh token rotation (DELETE ... RETURNING) before adding multi-device support.
- Add JWT algorithm validation (`WithValidMethods`) in the same PR that adds typed claims.

**During monitoring setup:**
- Instrument NATS executor latency histogram before adding any business metrics.
- Write the alert runbook before deploying the alert. No runbook = no alert.
- Test health checks by actually killing each dependency and verifying the endpoint returns 503.

**During testing phase:**
- Write the NATS disconnect integration test before hardening retry logic — you need the test to validate the fix.
- Add VK integration tests to a scheduled CI job (not PR checks) to avoid token expiry disrupting PRs.
- Track branch coverage for crypto and middleware packages specifically.

**Ongoing maintenance:**
- Monitor VK API changelog monthly. Subscribe to Yandex.Business developer blog.
- Run the Yandex.Business smoke test weekly in a local environment (requires manual cookie refresh).
- Rotate JWT_SECRET quarterly using the dual-key rotation procedure. Document the last rotation date in the runbook.
- Review Prometheus cardinality monthly: `prometheus_tsdb_head_series` should not grow faster than the user count.
