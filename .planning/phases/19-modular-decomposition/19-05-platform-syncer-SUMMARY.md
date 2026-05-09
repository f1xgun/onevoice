---
phase: 19
plan: 19-05
slug: platform-syncer
subsystem: services/api/internal/platform
tags: [refactor, capability-interfaces, platform-sync, no-behavior-change]
wave: 1
depends_on: []
status: complete
completed: 2026-05-09
requires: []
provides:
  - platform.TitleSyncer
  - platform.DescriptionSyncer
  - platform.PhotoSyncer
  - platform.ScheduleSyncer
  - platform.InfoSyncer
  - platform.Syncer (capability dispatcher)
  - platform.TelegramSyncer
  - platform.VKSyncer
  - platform.YandexSyncer
affects:
  - services/api/cmd/main.go
  - services/api/internal/platform/*
key-files:
  created:
    - services/api/internal/platform/syncer.go
    - services/api/internal/platform/telegram_syncer.go
    - services/api/internal/platform/vk_syncer.go
    - services/api/internal/platform/yandex_syncer.go
    - services/api/internal/platform/helpers.go
  modified:
    - services/api/cmd/main.go
    - services/api/internal/platform/sync_yandex_test.go
  deleted:
    - services/api/internal/platform/sync.go
decisions:
  - id: D-10
    title: capability-segregated interfaces
    rationale: Each platform implements only what it supports; SyncBusiness type-asserts per capability — no no-op stub methods.
  - id: R5
    title: VK groups.edit stays as a single InfoSyncer call
    rationale: Splitting the batched description+phone+website mutation into per-field syncs would 3× the API quota for no benefit.
metrics:
  duration: ~50 minutes
  tasks: 1
  files-created: 5
  files-modified: 2
  files-deleted: 1
  loc-delta: -642 / +682 (sync.go 642 LOC → syncer.go 263 + telegram 241 + vk 133 + yandex 79 + helpers 197 = 913 across five files; old sync.go absorbed into per-platform splits)
success-criteria: [SC-01, SC-02, SC-03]
---

# Phase 19 Plan 19-05: PlatformSyncer Capability Interfaces Summary

PlatformSyncer switch dispatch decomposed into capability-segregated interfaces (TitleSyncer / DescriptionSyncer / PhotoSyncer / ScheduleSyncer / InfoSyncer); per-platform impls live in dedicated files and declare only the capabilities they implement via compile-time assertions.

## What Changed

`services/api/internal/platform/sync.go` (642 LOC, switch-based) is gone. Replaced by:

| File                | LOC | Purpose                                                                                    |
| ------------------- | --- | ------------------------------------------------------------------------------------------ |
| `syncer.go`         | 263 | Capability interfaces + `Syncer` struct + `SyncBusiness` dispatch + `runWithTask` wrapper. |
| `telegram_syncer.go`| 241 | `TelegramSyncer` implementing `TitleSyncer + DescriptionSyncer + PhotoSyncer`.             |
| `vk_syncer.go`      | 133 | `VKSyncer` implementing only `InfoSyncer` (groups.edit batched call — R5).                 |
| `yandex_syncer.go`  |  79 | `YandexSyncer` implementing only `ScheduleSyncer` (RPA via NATS).                          |
| `helpers.go`        | 197 | `formatTelegramDescription`, `formatSchedule`, `scheduleToYandexJSON`, day mappings.       |

Compile-time assertions:

```go
var _ TitleSyncer       = (*TelegramSyncer)(nil)
var _ DescriptionSyncer = (*TelegramSyncer)(nil)
var _ PhotoSyncer       = (*TelegramSyncer)(nil)
var _ InfoSyncer        = (*VKSyncer)(nil)
var _ ScheduleSyncer    = (*YandexSyncer)(nil)
```

`SyncBusiness` walks active integrations, looks up the platform impl in `perPlatform map[string]any`, and type-asserts each capability — every capability the impl satisfies runs as an independent task. Telegram doesn't implement `ScheduleSyncer`, VK doesn't implement title/description/photo individually, Yandex doesn't implement title/description/photo/info — those branches are simply skipped, no no-op methods needed.

## Wiring change (services/api/cmd/main.go)

```go
integrationAdapter := &integrationSyncAdapter{svc: integrationService}
platformHTTPClient := &http.Client{Timeout: 10 * time.Second}
var agentTaskPublisher *platform.NATSTaskPublisher
if natsConn != nil {
    agentTaskPublisher = platform.NewNATSTaskPublisher(natsConn)
}
var yandexPublisher platform.TaskPublisher
if agentTaskPublisher != nil {
    yandexPublisher = agentTaskPublisher
}
perPlatform := map[string]any{
    a2a.AgentTelegram:       platform.NewTelegramSyncer(integrationAdapter, platformHTTPClient, "", cfg.PublicURL),
    a2a.AgentVK:             platform.NewVKSyncer(integrationAdapter, platformHTTPClient, ""),
    a2a.AgentYandexBusiness: platform.NewYandexSyncer(yandexPublisher),
}
platformSyncer := platform.NewSyncer(integrationAdapter, agentTaskRepo, taskHub, perPlatform)
```

Setters (`SetTaskRecorder`, `SetTaskHub`, `SetTaskPublisher`) removed in favor of constructor injection. Required deps (`integrations`, `tasks`) panic on nil; `hub` and `taskPublisher` may be nil silently.

The `*platform.NATSTaskPublisher` is funneled through the `platform.TaskPublisher` interface variable to avoid a typed-nil pitfall (a `*T` that's nil isn't `== nil` when stored in an `interface{}` field).

## Behavior Preservation

Method bodies moved verbatim. AgentTask shapes (`Type`, `DisplayName`, `Input` map, `Error` string, `Status`) match the prior switch dispatch:

- `sync_title` error branch records only `{channel_id}`; done branch additionally records `name` (per sync.go:138-143).
- `sync_description`, `sync_photo` always record `{channel_id}`.
- `sync_info` (VK) records `{group_id, description, phone}` plus `website` when `b.Website != nil`.
- `sync_hours` (Yandex) records `{permalink, hours}`; **silent skip** preserved when `scheduleToYandexJSON(b.Settings) == ""`.
- VK token-fetch error keeps the `"token fetch failed: <inner>"` prefix (via `errTokenFetchFailed` wrapping).
- Yandex publisher-nil keeps the `"NATS task publisher not configured"` error string verbatim (test `TestSyncBusiness_TaskPublisherNil_RecordsErrorTask` continues to pass unchanged).

Telegram's three Bot API calls (`setChatTitle`, `setChatDescription`, `setChatPhoto`), VK's `groups.edit` via `callVKAPI`, and Yandex's `a2a.RequestTool(yandex_business__update_hours)` ship the same parameters, headers, and timeouts as before.

## Test Updates (D-16: import-path-only changes)

`sync_yandex_test.go` keeps every assertion identical. The constructor calls migrated from:

```go
s := NewSyncer(integ, nil, "")
s.SetTaskRecorder(rec)
s.SetTaskPublisher(pub)
```

to a single helper:

```go
s := newYandexOnlySyncer(integ, rec, pub)  // wires perPlatform with NewYandexSyncer(pub)
```

All four existing test functions (`TestScheduleToYandexJSON`, `TestSyncBusiness_PublishesYandexHours`, `TestSyncBusiness_NoYandexIntegration_NoPublish`, `TestSyncBusiness_TaskPublisherNil_RecordsErrorTask`, `TestSyncBusiness_AgentReturnsError_RecordsErrorTask`) pass with **assertions unchanged**.

## Verification Results

| Gate                                                     | Status | Notes                                          |
| -------------------------------------------------------- | ------ | ---------------------------------------------- |
| `cd services/api && GOWORK=off go build ./...`           | green  | 0 errors                                       |
| `cd services/api && GOWORK=off go test -race ./internal/platform/...` | green  | All tests pass                                 |
| `cd services/api && GOWORK=off go test -race ./...`      | green  | All API packages pass                          |
| `make lint-all` (Go modules)                             | green  | 0 issues across pkg + 4 services               |
| `make test-all` (Go modules)                             | green  | All Go tests pass                              |
| `test ! -f services/api/internal/platform/sync.go`       | PASS   | File deleted                                   |
| Each `*_syncer.go` ≤ 500 LOC                             | PASS   | Max 263 (syncer.go); rest 79–241               |
| Capability assertions (regex `^var _ XSyncer\b`)         | PASS   | Each of 5 returns 1                            |
| VK forbids Title/Description/Photo (R5)                  | PASS   | 0 hits                                         |

## Deviations from Plan

**1. [Rule 3 - Blocking] Updated `services/api/cmd/main.go` directly instead of `services/api/internal/wire/services.go`**

- **Why:** Plan-step 7 says "Update construction in `services/api/internal/wire/services.go` (created by 19-01)". `wire/` does not exist on this branch yet (plan 19-01 has not landed) and 19-05's `depends_on: []` declares it must be standalone.
- **Resolution:** Wired `perPlatform` map directly in `services/api/cmd/main.go`. When 19-01 lands, the same construction block moves to `wire/services.go` verbatim.

**2. [Rule 2 - Critical functionality] Typed-nil interface trap guard in main.go wiring**

- **Why:** `var p *platform.NATSTaskPublisher = nil; var iface platform.TaskPublisher = p; iface == nil` is **false** (typed nil != untyped nil). The original code used setters so this didn't surface; the new constructor takes `TaskPublisher` directly. Without a guard, `YandexSyncer.taskPublisher == nil` check would always evaluate false even when natsConn was nil, producing real RPC attempts against a closed conn.
- **Resolution:** Funnel `agentTaskPublisher` through a `platform.TaskPublisher` variable that is left as the zero `nil` when `natsConn == nil`.

**3. [Rule 2 - Critical functionality] Empty-schedule silent-skip moved to dispatcher**

- **Why:** Original `syncYandexHours` returned early on empty `hoursJSON` without recording an AgentTask. With the capability-interface protocol, returning early from `SyncSchedule` would cause the dispatcher to record a `done` task — silent change in observable behavior.
- **Resolution:** Hoisted the empty-schedule check into `dispatchCapabilities`. `SyncSchedule` is now only called when there's real work; the "no noisy AgentTask row" invariant is preserved.

**4. [Rule 3 - Blocking] Removed `string(a2a.AgentXxx)` conversions**

- **Why:** `pkg/a2a/protocol.go` declares `type AgentID = string` (alias, not defined type). `unconvert` linter flags `string(a2a.AgentTelegram)` as unnecessary. Without removal, `make lint-all` fails.
- **Resolution:** Use `a2a.AgentTelegram` directly as `map[string]any` keys (compile-checked: alias means literal string).

**5. [Rule 3 - Blocking] gofmt fixup on syncer.go**

- **Why:** `make lint-all` flagged trailing-whitespace alignment on the `Syncer` struct.
- **Resolution:** `gofmt -w` applied; comments now align consistently.

## Deferred Issues

None blocking. One pre-existing environment issue documented in `deferred-items.md`:
- Frontend lint/test fail in fresh agent worktrees because `services/frontend/node_modules` isn't installed. Out of scope for 19-05 (Go-only refactor); affects all backend plans equally and will be resolved when frontend plans 19-10/11/12 prepare their environment.

## Commits

| Plan task | Hash      | Subject                                                           |
| --------- | --------- | ----------------------------------------------------------------- |
| 19-05-01  | b9a72c6   | refactor(19): convert platform.Syncer to capability-interface strategy |

## Self-Check: PASSED

- File `services/api/internal/platform/sync.go` absent: verified.
- Five new files present: verified.
- Capability assertions match plan regex (5×1): verified.
- VK forbids Title/Description/Photo: verified (0 hits).
- LOC ≤ 500 for every file in `internal/platform/`: verified.
- `cd services/api && GOWORK=off go test -race ./...` green: verified.
- `make lint-all` (Go) green: verified.
- Commit b9a72c6 exists in `git log`: verified.
