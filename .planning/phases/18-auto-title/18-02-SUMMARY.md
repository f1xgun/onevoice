---
phase: 18
plan: 02
subsystem: api/config + api/wiring
tags: [config, llm-router, wiring, api, auto-titler]
requirements: [TITLE-02]
dependency-graph:
  requires:
    - "Phase 15 conversation domain (TitleStatus enum on Conversation)"
    - "services/orchestrator/internal/config/config.go (verbatim source for SelfHostedEndpoint + parseIndexedEndpoints + provider key set)"
    - "services/orchestrator/cmd/main.go:740-788 (verbatim source for buildProviderOpts)"
  provides:
    - "config.Config.TitlerModel + LLMModel + LLMTier + provider keys + SelfHostedEndpoints (services/api side)"
    - "services/api/cmd/main.go owns a *llm.Router local var (currently placeholder via _ = llmRouter; Plan 18-04 consumes)"
    - "Insertion-point comment for Plan 18-03's EnsureConversationIndexes (Phase 18 D-08a Mongo index)"
  affects:
    - "Plan 18-04 (Titler service) — will replace `_ = llmRouter` with service.NewTitler(llmRouter, conversationRepo, titlerModel)"
    - "Plan 18-03 (atomic Mongo update + index helper) — fills in the reserved comment with EnsureConversationIndexes call"
    - "Plan 18-05 (chat_proxy trigger gate) — relies on titler nil-safety for graceful disable"
tech-stack:
  added:
    - "github.com/f1xgun/onevoice/pkg/llm (FIRST import in services/api per Landmine 3)"
    - "github.com/f1xgun/onevoice/pkg/llm/providers (transitive: github.com/sashabaranov/go-openai, github.com/anthropics/anthropic-sdk-go via go.sum refresh)"
  patterns:
    - "Verbatim lift of buildProviderOpts and parseIndexedEndpoints from orchestrator (parity preserved for future audits)"
    - "Graceful disable via local-var placeholder + warning logs (never fail-fast on missing optional env)"
key-files:
  created:
    - "services/api/internal/config/config_test.go (Phase 18 fields exercised end-to-end with t.Setenv)"
  modified:
    - "services/api/internal/config/config.go (+76 lines: SelfHostedEndpoint type, 7 Config fields, env loading block, parseIndexedEndpoints helper)"
    - "services/api/cmd/main.go (+114 lines: pkg/llm + pkg/llm/providers imports, Router-construction block, buildProviderOpts helper, EnsureConversationIndexes comment placeholder)"
    - "services/api/go.mod + services/api/go.sum (refreshed via `go mod tidy` to add LLM SDK deps for the FIRST pkg/llm import in services/api)"
decisions:
  - "Graceful disable on TWO distinct branches with distinct log copy: `auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)` AND `auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)`. Observability can distinguish 'no model' vs 'no key' from a single grep."
  - "buildProviderOpts lifted verbatim — body byte-identical to orchestrator's so future audits diff cleanly. Only intentional difference is package locality."
  - "_ = llmRouter placeholder accepted for the build to pass without Plan 18-04. Plan 18-04 contract: replace the underscore line with `titler := service.NewTitler(llmRouter, conversationRepo, titlerModel)` and thread `titler` through chat_proxy/handler.NewTitlerHandler."
  - "Insertion-point comment 'Phase 18 Plan 03: insert EnsureConversationIndexes here.' reserves spot at line 118 (immediately after EnsurePendingToolCallsIndexes block ends) per Plan 03 contract."
metrics:
  duration: "~25min wall clock"
  completed: "2026-04-26T18:12:19Z"
  tasks: 2
  commits: 2
  files_created: 1
  files_modified: 3
---

# Phase 18 Plan 02: Auto-Titler Config + LLM Router Wiring Summary

API service now owns a `*llm.Router` local variable in `cmd/main.go` and 7 new `Config` fields with TITLER_MODEL → LLM_MODEL fallback semantics; Phase 18 Plan 04 has its contract.

## Final Config field set (Phase 18 additions)

All seven fields appended to the existing `services/api/internal/config/config.go` `Config` struct:

| Field                  | Env var               | Default / fallback                                  |
| ---------------------- | --------------------- | --------------------------------------------------- |
| `LLMModel`             | `LLM_MODEL`           | empty (not required — graceful disable)            |
| `LLMTier`              | `LLM_TIER`            | `"free"` if unset                                   |
| `TitlerModel`          | `TITLER_MODEL`        | falls back to `LLMModel` if unset; both empty → "" |
| `OpenRouterAPIKey`     | `OPENROUTER_API_KEY`  | empty (optional)                                    |
| `OpenAIAPIKey`         | `OPENAI_API_KEY`      | empty (optional)                                    |
| `AnthropicAPIKey`      | `ANTHROPIC_API_KEY`   | empty (optional)                                    |
| `SelfHostedEndpoints`  | `SELF_HOSTED_N_URL/MODEL/API_KEY` (N=0..) | indexed scan; entries without MODEL skipped; scan stops at first missing URL |

The new `SelfHostedEndpoint` type and `parseIndexedEndpoints()` helper are lifted byte-identical from `services/orchestrator/internal/config/config.go` lines 39-44 and 140-159.

**Critical:** No new validation added. Pitfall 1 / Assumption A6 mandates graceful disable — the API service must boot when none of these env vars are set. The orchestrator-side `LLM_MODEL is required` validation is intentionally NOT mirrored here.

## buildProviderOpts source range (lifted verbatim)

| Source                                              | Range          | Destination                            |
| --------------------------------------------------- | -------------- | -------------------------------------- |
| `services/orchestrator/cmd/main.go::buildProviderOpts` | lines 740-788  | `services/api/cmd/main.go` line 735+   |

Lifted verbatim with only the package-local config import path differing. Body, signature, registry-registration semantics, self-hosted nil check (`if p == nil { ... continue }`), and log lines are byte-identical to the orchestrator version. Future audits can `diff` the two functions and expect zero meaningful drift.

## Graceful-disable warning copy (verified via grep)

Two log lines fire from distinct branches at startup, each verifiable as a single grep:

1. **`TITLER_MODEL == ""` AND `LLM_MODEL == ""`** (no model at all):
   ```
   auto-titler: disabled (TITLER_MODEL and LLM_MODEL both unset)
   ```
   Logged via `log.Warn(...)` at `services/api/cmd/main.go` ~line 198.

2. **Model set, but no provider key set** (most common dev miss):
   ```
   auto-titler: disabled (no LLM provider API key set; set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY to enable)
   ```
   Logged via `log.Warn(...)` at `services/api/cmd/main.go` ~line 194.

3. **Successful router construction:**
   ```
   auto-titler: llm router constructed model=<TITLER_MODEL> providers=<count>
   ```
   Logged via `log.Info(...)` at `services/api/cmd/main.go` ~line 193. NEVER references API-key values themselves — threat T-18-05 mitigated.

## Plan 04 / Plan 03 contract handoffs

- **Plan 18-04 contract:** local var `llmRouter *llm.Router` is in main scope after the auto-titler wiring block (line 186). Replace the placeholder `_ = llmRouter` line (~line 200) with `titler := service.NewTitler(llmRouter, conversationRepo, titlerModel)` and thread `titler` into the chat_proxy + new TitlerHandler constructors. The `titlerModel` local var is in scope from line 187.
- **Plan 18-03 contract:** comment `// Phase 18 Plan 03: insert EnsureConversationIndexes here.` at line 118 marks the exact spot where the new index helper call must land — immediately after the existing `indexesCancel()` for `EnsurePendingToolCallsIndexes`, before `pendingToolCallRepo := …`.

## Verification Results

- `cd services/api && GOWORK=off go build ./...` — clean (after `go mod tidy` added LLM SDK deps; Landmine 3 closed)
- `cd services/api && GOWORK=off go vet ./...` — clean
- `cd services/api && GOWORK=off go test -race -count=1 ./internal/config/...` — `ok` (1.5s)
- Standalone binary build (`go build -o /tmp/api-bin ./cmd`) — succeeds; 48 MB executable produced
- All 11 acceptance-criteria greps from PLAN.md return the expected counts (`grep -F "os.Setenv" config_test.go` returns 0; `grep -F "auto-titler: disabled"` returns 2; `grep -nF "var llmRouter *llm.Router"` returns 1)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Refreshed services/api/go.sum for new pkg/llm/providers transitive deps**

- **Found during:** Task 2 verification (`go build` failed with "missing go.sum entry for module providing package github.com/anthropics/anthropic-sdk-go" + same for `github.com/sashabaranov/go-openai`)
- **Issue:** Adding `pkg/llm/providers` import on the API side pulled in two third-party LLM SDK transitives that services/api had never imported before — exactly the Landmine 3 the plan flagged.
- **Fix:** Ran `cd services/api && GOWORK=off go mod tidy` once. Re-built clean.
- **Files modified:** `services/api/go.mod`, `services/api/go.sum` (refresh only — no manual edits)
- **Commit:** `97e804a`
- **Rationale:** Plan documented this as Pitfall 1 / Landmine 3 ("services/api currently has ZERO `pkg/llm` imports"). Refreshing go.sum is the canonical fix; no code change needed.

### Out-of-scope discoveries

None — both tasks executed exactly as written in the PLAN action steps.

## Self-Check: PASSED

Created files exist:
- FOUND: `services/api/internal/config/config_test.go`
- FOUND: `.planning/phases/18-auto-title/18-02-SUMMARY.md` (this file)

Modified files exist with expected content:
- FOUND: `services/api/internal/config/config.go` (TitlerModel field at line 83, parseIndexedEndpoints at line 207)
- FOUND: `services/api/cmd/main.go` (var llmRouter at line 186, buildProviderOpts at line 735, Plan 03 reservation at line 118)
- FOUND: `services/api/go.mod` + `services/api/go.sum` (refreshed via tidy)

Commits exist:
- FOUND: `de53a24` — feat(18-02): extend services/api Config with auto-titler env
- FOUND: `97e804a` — feat(18-02): wire pkg/llm.Router + buildProviderOpts into services/api

## Threat Flags

None. Plan 18-02's surface (config env loading + LLM Router wiring) was already enumerated in the threat_model:
- T-18-05 (provider-key leak in startup logs): mitigated — log lines reference only `model` and `providers` count; never the API keys themselves. Verified by grep — `grep -nF "OpenRouterAPIKey" services/api/cmd/main.go` returns only field-access lines (in `buildProviderOpts` and the SelfHostedEndpoints loop), never inside a `log.*` call.
- T-18-06 (graceful-disable bypass): accepted per single-owner v1.3 deployment trust model.

No new network endpoints, auth paths, or schema changes introduced — config additions are env-readers only; the Router construction is pure in-process composition.
