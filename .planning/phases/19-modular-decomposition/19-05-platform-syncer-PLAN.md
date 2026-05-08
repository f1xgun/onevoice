---
plan: 19-05
phase: 19
slug: platform-syncer
wave: 1
depends_on: []
files_modified:
  - services/api/internal/platform/sync.go
files_created:
  - services/api/internal/platform/syncer.go
  - services/api/internal/platform/telegram_syncer.go
  - services/api/internal/platform/vk_syncer.go
  - services/api/internal/platform/yandex_syncer.go
  - services/api/internal/platform/helpers.go
files_deleted:
  - services/api/internal/platform/sync.go
success_criteria: [SC-01, SC-02, SC-03]
autonomous: true
estimated_loc_delta: -640 / +700
---

## Plan Goal

Convert `services/api/internal/platform/sync.go` (640 LOC, switch-based dispatch) into a strategy-pattern with **capability-segregated interfaces**: `TitleSyncer`, `DescriptionSyncer`, `PhotoSyncer`, `ScheduleSyncer`, plus an `InfoSyncer` for batched-update platforms (VK's `groups.edit`). Each platform implements only the capabilities it supports; `Syncer.SyncBusiness()` does type-assertion dispatch per capability — no no-op methods.

Implements: D-10 (capability-segregated interfaces) + R5 from 19-RESEARCH.md (preserve VK batched `InfoSyncer`).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/api/internal/platform/sync.go
@services/api/AGENTS.md
@docs/go-style.md
</context>

<tasks>

<task type="auto">
  <id>19-05-01</id>
  <title>Extract platform syncer capability interfaces + per-platform impls + helpers</title>
  <wave>1</wave>
  <read_first>
    - services/api/internal/platform/sync.go (full file — currently 640 LOC; method-by-method extraction targets)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 8 "PlatformSyncer Capability Interfaces" lines 1007-1119)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-05" lines 545-635)
  </read_first>
  <action>
    Refactor `services/api/internal/platform/` into 5 files. Move method bodies VERBATIM. No logic edits.

    1. **`platform/syncer.go`** — orchestration + capability interfaces:
       ```go
       package platform

       type TitleSyncer       interface { SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error }
       type DescriptionSyncer interface { SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error }
       type PhotoSyncer       interface { SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error }
       type ScheduleSyncer    interface { SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error }
       type InfoSyncer        interface { SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error }

       type integrationProvider interface {
           ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
       }

       type taskRecorder interface {
           Create(ctx context.Context, task *domain.AgentTask) error
       }

       type Syncer struct {
           integrations integrationProvider
           tasks        taskRecorder
           hub          *taskhub.Hub
           perPlatform  map[string]any // string → *TelegramSyncer | *VKSyncer | *YandexSyncer | ... (any of the capability-implementing types)
       }

       func NewSyncer(integrations integrationProvider, tasks taskRecorder, hub *taskhub.Hub, perPlatform map[string]any) *Syncer {
           if integrations == nil { panic("platform.NewSyncer: integrations cannot be nil") }
           if tasks == nil { panic("platform.NewSyncer: tasks cannot be nil") }
           // hub optional
           return &Syncer{integrations: integrations, tasks: tasks, hub: hub, perPlatform: perPlatform}
       }

       func (s *Syncer) SyncBusiness(business *domain.Business) {
           ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
           defer cancel()
           integrations, err := s.integrations.ListByBusinessID(ctx, business.ID)
           if err != nil { /* preserve current log shape */ ; return }
           for _, integ := range integrations {
               if integ.Status != "active" { continue }
               platImpl, ok := s.perPlatform[integ.Platform]
               if !ok { continue }
               if t, ok := platImpl.(TitleSyncer); ok       { s.runWithTask(ctx, business, integ, "sync_title",       t.SyncTitle) }
               if d, ok := platImpl.(DescriptionSyncer); ok { s.runWithTask(ctx, business, integ, "sync_description", d.SyncDescription) }
               if p, ok := platImpl.(PhotoSyncer); ok && business.LogoURL != "" { s.runWithTask(ctx, business, integ, "sync_photo", p.SyncPhoto) }
               if i, ok := platImpl.(InfoSyncer); ok        { s.runWithTask(ctx, business, integ, "sync_info",        i.SyncInfo) }
               if sch, ok := platImpl.(ScheduleSyncer); ok  { s.runWithTask(ctx, business, integ, "sync_hours",       sch.SyncSchedule) }
           }
       }

       // runWithTask wraps a SyncXxx call with start time, error capture, taskhub
       // publish + AgentTask repo create. Lift current `recordTask` shape from
       // sync.go:90-114 verbatim — same fields, same publish order.
       func (s *Syncer) runWithTask(ctx context.Context, b *domain.Business, integ domain.Integration, action string, fn func(context.Context, *domain.Business, domain.Integration) error) { /* ... */ }
       ```

    2. **`platform/telegram_syncer.go`** — `TelegramSyncer` implementing Title/Description/Photo:
       ```go
       type TelegramSyncer struct {
           httpClient   *http.Client
           telegramBase string
           publicURL    string
       }

       var _ TitleSyncer       = (*TelegramSyncer)(nil)
       var _ DescriptionSyncer = (*TelegramSyncer)(nil)
       var _ PhotoSyncer       = (*TelegramSyncer)(nil)

       func NewTelegramSyncer(httpClient *http.Client, telegramBase, publicURL string) *TelegramSyncer { /* ... */ }

       func (t *TelegramSyncer) SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error {
           // body lifts current syncTelegramTitle (sync.go:285) — same Telegram API call, same params
       }
       func (t *TelegramSyncer) SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error {
           // lifts syncTelegramDescription (sync.go:326)
       }
       func (t *TelegramSyncer) SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error {
           // lifts syncTelegramPhoto (sync.go:367)
       }
       ```

    3. **`platform/vk_syncer.go`** — `VKSyncer` implementing only InfoSyncer (per RESEARCH §8 R5: `groups.edit` is a single batched API call; do NOT split into per-field calls):
       ```go
       type VKSyncer struct {
           httpClient *http.Client
           vkBase     string
       }

       var _ InfoSyncer = (*VKSyncer)(nil)

       func (v *VKSyncer) SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error {
           // lifts syncVKInfo (sync.go:445) — single groups.edit call setting description+phone+website
       }
       ```

    4. **`platform/yandex_syncer.go`** — `YandexSyncer` implementing only ScheduleSyncer:
       ```go
       type YandexSyncer struct {
           taskPublisher AgentTaskPublisher // NATS A2A
       }

       var _ ScheduleSyncer = (*YandexSyncer)(nil)

       func (y *YandexSyncer) SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error {
           // lifts syncYandexHours (sync.go:593) — RPA NATS dispatch; same a2a.RequestTool shape
       }
       ```

    5. **`platform/helpers.go`** — package-private utilities lifted verbatim from sync.go: `formatTelegramDescription`, `formatSchedule`, `dayKeyToEnglish`, `scheduleToYandexJSON`, `callVKAPI` (current names preserved).

    6. **Delete `services/api/internal/platform/sync.go`** — every line moved.

    7. **Update construction in `services/api/internal/wire/services.go`** (created by 19-01): the current Syncer constructor invocation `platform.NewSyncer(integrations, tasks, hub)` becomes:
       ```go
       perPlatform := map[string]any{
           "telegram":         platform.NewTelegramSyncer(httpClient, cfg.TelegramAPIBase, cfg.PublicURL),
           "vk":               platform.NewVKSyncer(httpClient, cfg.VKAPIBase),
           "yandex_business":  platform.NewYandexSyncer(taskPublisher),
       }
       platformSyncer := platform.NewSyncer(integrations, agentTaskRepo, taskHub, perPlatform)
       ```

    Apply project conventions:
    - Compile-time interface checks (`var _ TitleSyncer = (*TelegramSyncer)(nil)`) at the bottom of each `*_syncer.go`.
    - Constructor panics on nil required deps; `taskHub`/`taskPublisher` optional silently nil-accepted.
    - Error wrapping `fmt.Errorf("platform: <verb>: %w", err)`.
    - Imports stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → `github.com/f1xgun/onevoice/services/api/...`.

    Anti-pattern (RESEARCH §8 R5 — re-stated):
    - Do NOT split VK's `groups.edit` into per-capability calls. Single `InfoSyncer.SyncInfo` is correct.
    - Do NOT add no-op stub methods (e.g., `func (v *VKSyncer) SyncTitle()`). Type-assertion dispatch handles "platform doesn't support this capability" by simply not implementing it.

    Commit subject: `refactor(19): convert platform.Syncer to capability-interface strategy`.
  </action>
  <acceptance_criteria>
    - File `services/api/internal/platform/sync.go` does NOT exist: `test ! -f services/api/internal/platform/sync.go`
    - All five new files exist under `services/api/internal/platform/`
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/platform/...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0
    - `rg -c '^var _ TitleSyncer\b' services/api/internal/platform/telegram_syncer.go` returns 1
    - `rg -c '^var _ DescriptionSyncer\b' services/api/internal/platform/telegram_syncer.go` returns 1
    - `rg -c '^var _ PhotoSyncer\b' services/api/internal/platform/telegram_syncer.go` returns 1
    - `rg -c '^var _ InfoSyncer\b' services/api/internal/platform/vk_syncer.go` returns 1
    - `rg -c '^var _ ScheduleSyncer\b' services/api/internal/platform/yandex_syncer.go` returns 1
    - VK does NOT implement TitleSyncer/DescriptionSyncer/PhotoSyncer (RESEARCH R5): `rg 'var _ (TitleSyncer|DescriptionSyncer|PhotoSyncer) = \(\*VKSyncer\)' services/api/internal/platform/` returns 0
    - Each platform file ≤500 LOC: `wc -l services/api/internal/platform/*.go | awk '$2!="total" && $1>500 {exit 1}'`
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go test -race ./internal/platform/... &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

</tasks>

## Verification

```bash
# SC-01
test ! -f services/api/internal/platform/sync.go
wc -l services/api/internal/platform/*.go | awk '$2!="total" && $1>500 {print; exit 1}'

# SC-04 capability assertions
rg "var _ (Title|Description|Photo|Schedule|Info)Syncer" services/api/internal/platform/

# SC-02 + SC-03
make lint-all && make test-all
```

## Success Criteria

- `platform/sync.go` deleted; replaced by 5 files all ≤500 LOC
- Each capability implementation has compile-time interface assertion
- VK keeps batched `InfoSyncer` (R5 honored)
- All existing platform tests pass unchanged (SC-03)
- `make lint-all && make test-all` green (SC-02)
