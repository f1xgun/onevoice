---
plan: 19-04
phase: 19
slug: oauth-split
wave: 2
depends_on: [19-01, 19-03]
files_modified:
  - services/api/internal/handler/oauth.go
  - services/api/internal/router/router.go
  - services/api/internal/wire/handlers.go
files_created:
  - services/api/internal/handler/oauth/base.go
  - services/api/internal/handler/oauth/vk.go
  - services/api/internal/handler/oauth/yandex.go
  - services/api/internal/handler/oauth/google.go
  - services/api/internal/handler/connect/connect.go
  - services/api/internal/handler/connect/telegram.go
  - services/api/internal/handler/connect/vk_community.go
files_deleted:
  - services/api/internal/handler/oauth.go
success_criteria: [SC-01, SC-02, SC-03]
autonomous: true
estimated_loc_delta: -1700 / +1800
---

## Plan Goal

Split `services/api/internal/handler/oauth.go` (1703 LOC, 30 methods, 4 platforms × 2 flows fused) into:

- **`handler/oauth/`** sub-package (Go `package oauth`) — true OAuth code-flow platforms: VK, Yandex, Google. Receiver type `*OAuthHandler` (renamed only in import — keeps method names).
- **`handler/connect/`** sub-package (Go `package connect`) — paste-flow Connect platforms: Telegram (Login Widget verify + bot-token paste), VK community (community access token paste). NEW receiver type `*ConnectHandler` with narrower `ConnectConfig` (only `TelegramBotToken`, `VKServiceKey`, `FrontendURL`, plus testing-base-URL overrides).

**Public route paths (`/oauth/...`, `/integrations/...`) remain UNCHANGED** — only the handler struct that owns each route changes. Route table updated to dispatch paste-flow routes to `handlers.Connect` instead of `handlers.OAuth`.

Implements: D-04 (OAuth/Connect split), R1 from 19-RESEARCH.md (two-handler-types interpretation), Q3 (trim ConnectConfig).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/api/internal/handler/oauth.go
@services/api/internal/router/router.go
@services/api/AGENTS.md
</context>

<tasks>

<task type="auto">
  <id>19-04-01</id>
  <title>Create handler/oauth/ sub-package with base.go + vk.go + yandex.go + google.go</title>
  <wave>1</wave>
  <read_first>
    - services/api/internal/handler/oauth.go (full file — 1703 LOC; verify line ranges)
    - services/api/internal/router/router.go (lines 18-35 Handlers struct; lines 66-121 route registration)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 3 "Decomposition Pattern: oauth.go" lines 291-378)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-04 — oauth split" lines 449-541)
  </read_first>
  <action>
    Create new sub-package `services/api/internal/handler/oauth/` (Go package `oauth`). Four files. **Move method bodies verbatim**; no logic edits.

    1. **`oauth/base.go`** — struct + URL helpers + interfaces. Lifts oauth.go:30-156:
       ```go
       package oauth

       type OAuthConfig struct {
           VKClientID     string
           VKClientSecret string
           VKRedirectURI  string
           VKServiceKey   string  // VK only — keep here even though paste-flow also uses; ConnectConfig duplicates field
           YandexClientID string
           YandexClientSecret string
           YandexRedirectURI  string
           GoogleClientID     string
           GoogleClientSecret string
           GoogleRedirectURI  string
           FrontendURL        string
           // testing overrides
           vkAPIBaseURL          string
           vkTokenBaseURLOverride string
           yandexTokenURLOverride string
           googleTokenURLOverride string
           googleAccountsBaseURL  string
           googleBusinessInfoBase string
       }

       type OAuthHandler struct {
           oauthService       OAuthStateService
           integrationService OAuthIntegrationService
           businessService    BusinessService
           cfg                OAuthConfig
           httpClient         *http.Client
           redis              *goredis.Client
           taskPublisher      AgentTaskPublisher // optional
       }

       func NewOAuthHandler(
           oauthService OAuthStateService,
           integrationService OAuthIntegrationService,
           businessService BusinessService,
           cfg OAuthConfig,
           httpClient *http.Client,
           redis *goredis.Client,
       ) *OAuthHandler {
           if oauthService == nil { panic("oauth.NewOAuthHandler: oauthService cannot be nil") }
           if integrationService == nil { panic("oauth.NewOAuthHandler: integrationService cannot be nil") }
           if businessService == nil { panic("oauth.NewOAuthHandler: businessService cannot be nil") }
           if httpClient == nil { httpClient = &http.Client{Timeout: 10 * time.Second} }
           return &OAuthHandler{...}
       }

       func (h *OAuthHandler) WithAgentTaskPublisher(p AgentTaskPublisher) *OAuthHandler {
           h.taskPublisher = p
           return h
       }

       // URL helpers (oauth.go:131-156)
       func (h *OAuthHandler) vkAPIBase() string         { /* verbatim */ }
       func (h *OAuthHandler) vkTokenBaseURL() string    { /* verbatim */ }
       func (h *OAuthHandler) yandexTokenURL() string    { /* verbatim */ }
       func (h *OAuthHandler) googleTokenURL() string    { /* verbatim */ }
       func (h *OAuthHandler) googleAccountsURL() string { /* verbatim */ }
       func (h *OAuthHandler) googleBusinessInfoURL() string { /* verbatim */ }

       // Service interfaces (defined where consumed per CONVENTIONS.md §Service Interfaces)
       type OAuthStateService interface { /* lift current shape from oauth.go */ }
       type OAuthIntegrationService interface { /* lift current shape from oauth.go */ }
       type BusinessService interface { /* lift current shape from oauth.go */ }
       type AgentTaskPublisher interface { /* lift current shape from oauth.go */ }

       // Internal types lifted: googleTempData, googleLocationRef, googleAccount
       type googleTempData struct { /* lift verbatim */ }
       type googleLocationRef struct { /* lift verbatim */ }
       type googleAccount struct { /* lift verbatim */ }
       ```

    2. **`oauth/vk.go`** — methods lifted from oauth.go:162-983 verbatim, except for the two paste-flow ones (`ConnectVK` line 537, `RefreshVKCommunityName` line 910 — those go to `connect/vk_community.go`):
       - `GetVKAuthURL` (oauth.go:162)
       - `VKCallback` (oauth.go:207)
       - `VKCommunities` (oauth.go:284)
       - `VKCommunityAuthURL` (oauth.go:350)
       - `VKCommunityCallback` (oauth.go:403)
       - `probeVKCommunityToken` (oauth.go:622)
       - `checkVKWallScope` (oauth.go:685)
       - `resolveVKGroupID` (oauth.go:804)
       - `fetchVKCommunityName` (oauth.go:861)

       All methods stay on receiver `*OAuthHandler` (Go method-receiver constraint — same package as base.go).

    3. **`oauth/yandex.go`** — `GetYandexAuthURL` (oauth.go:1198) and `YandexCallback` (oauth.go:1237). Verbatim move.

    4. **`oauth/google.go`** — `GetGoogleAuthURL` (oauth.go:1343), `GoogleCallback` (oauth.go:1384), `googleDiscoverAccounts` (oauth.go:1529), `googleDiscoverLocations` (oauth.go:1554), `GoogleLocations` (oauth.go:1579), `GoogleSelectLocation` (oauth.go:1622). Verbatim.

    Imports per file: stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → `github.com/f1xgun/onevoice/services/api/internal/...`. Each file imports only the minimum subset its moved methods need.

    DO NOT touch route table or handler.go consumers in this task. The old `services/api/internal/handler/oauth.go` is still untouched at this point.

    Commit subject: `refactor(19): create handler/oauth/ sub-package (base + vk + yandex + google)`.
  </action>
  <acceptance_criteria>
    - All four files exist under `services/api/internal/handler/oauth/`
    - `cd services/api && GOWORK=off go build ./internal/handler/oauth/...` exits 0
    - `cd services/api && GOWORK=off go vet ./internal/handler/oauth/...` exits 0
    - Each oauth/*.go file ≤500 LOC: `wc -l services/api/internal/handler/oauth/*.go | awk '$2!="total" && $1>500 {exit 1}'`
    - `rg -c '^func NewOAuthHandler\(' services/api/internal/handler/oauth/base.go` returns 1
    - `rg -c '^func \(h \*OAuthHandler\) (GetVKAuthURL|VKCallback|VKCommunities|VKCommunityAuthURL|VKCommunityCallback)\(' services/api/internal/handler/oauth/vk.go | head -1` returns ≥1
    - `rg -c '^func \(h \*OAuthHandler\) (GetYandexAuthURL|YandexCallback)\(' services/api/internal/handler/oauth/yandex.go` returns ≥1
    - `rg -c '^func \(h \*OAuthHandler\) (GetGoogleAuthURL|GoogleCallback|GoogleLocations|GoogleSelectLocation)\(' services/api/internal/handler/oauth/google.go` returns ≥1
    - The original `services/api/internal/handler/oauth.go` still compiles (we haven't deleted it yet — wired up in next task)
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go build ./internal/handler/oauth/...</automated>
</task>

<task type="auto">
  <id>19-04-02</id>
  <title>Create handler/connect/ sub-package with ConnectHandler + telegram + vk_community</title>
  <wave>1</wave>
  <read_first>
    - services/api/internal/handler/oauth.go:537-636,721-1195,910-983 (paste-flow methods + types)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 3 "Recommended interpretation of D-04" lines 348-365; Q3 lines 1791-1796)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md (lines 500-541)
  </read_first>
  <action>
    Create new sub-package `services/api/internal/handler/connect/` (Go package `connect`). Three files.

    1. **`connect/connect.go`** — `ConnectHandler` struct + narrower `ConnectConfig`:
       ```go
       package connect

       type ConnectConfig struct {
           TelegramBotToken string
           VKServiceKey     string
           FrontendURL      string
           // testing overrides
           vkAPIBaseURL       string
           telegramAPIBaseURL string
       }

       type ConnectHandler struct {
           integrationService ConnectIntegrationService
           businessService    BusinessService
           cfg                ConnectConfig
           httpClient         *http.Client
       }

       func NewConnectHandler(
           integrationService ConnectIntegrationService,
           businessService    BusinessService,
           cfg ConnectConfig,
           httpClient *http.Client,
       ) *ConnectHandler {
           if integrationService == nil { panic("connect.NewConnectHandler: integrationService cannot be nil") }
           if businessService == nil { panic("connect.NewConnectHandler: businessService cannot be nil") }
           if httpClient == nil { httpClient = &http.Client{Timeout: 10 * time.Second} }
           return &ConnectHandler{...}
       }

       // Service interfaces (defined where consumed)
       type ConnectIntegrationService interface { /* lift only methods used by paste-flow methods */ }
       type BusinessService interface { /* lift only methods used by paste-flow methods */ }

       // Internal types lifted from oauth.go used by paste-flow:
       type connectTelegramRequest  struct { /* lift from oauth.go */ }
       type refreshTelegramRequest  struct { /* lift from oauth.go */ }
       type telegramChatInfo        struct { /* lift from oauth.go */ }
       type telegramGetChatResponse struct { /* lift from oauth.go */ }
       type connectVKRequest        struct { /* lift from oauth.go */ }
       type vkGroup                 struct { /* lift from oauth.go */ }
       ```
       Per RESEARCH §16 Q3: `ConnectConfig` is a NARROW subset (only fields paste-flow needs). DO NOT pass full `OAuthConfig`.

    2. **`connect/telegram.go`** — methods lifted from oauth.go verbatim, only receiver type changes from `*OAuthHandler` to `*ConnectHandler`:
       - `VerifyTelegramLogin` (oauth.go:721)
       - `telegramGetChat` (oauth.go:987)
       - `probeTelegramLinkedGroup` (oauth.go:1027)
       - `ConnectTelegram` (oauth.go:1038)
       - `RefreshTelegramLinkedGroup` (oauth.go:1114)

       Inside method bodies: replace `h.cfg.TelegramBotToken` references — they resolve identically since `ConnectConfig.TelegramBotToken` exists. Replace `h.cfg.FrontendURL` similarly. URL helpers (`telegramAPIBaseURL`) — since `ConnectHandler` doesn't share `*OAuthHandler`'s helper methods, lift the equivalent helpers as private functions or methods on `*ConnectHandler` in `connect.go` (e.g., `func (h *ConnectHandler) telegramAPIBase() string`).

    3. **`connect/vk_community.go`** — paste-flow VK methods lifted verbatim, receiver `*ConnectHandler`:
       - `ConnectVK` (oauth.go:537)
       - `RefreshVKCommunityName` (oauth.go:910)

       Same VKServiceKey resolution: `h.cfg.VKServiceKey`. Same `h.vkAPIBase()` helper duplicated as a method on `*ConnectHandler` in `connect.go`.

    Anti-pattern (per 19-RESEARCH.md and 19-PATTERNS.md):
    - Do NOT pass full `OAuthConfig` to ConnectHandler. Use the narrow `ConnectConfig`.
    - Do NOT change route URL paths (those stay in router.go).
    - Do NOT rename methods (route table dispatches by method name).

    DO NOT update router.go or wire/handlers.go yet — that's the next task. The original oauth.go still compiles unchanged.

    Commit subject: `refactor(19): create handler/connect/ sub-package (telegram + vk_community paste-flow)`.
  </action>
  <acceptance_criteria>
    - All three files exist under `services/api/internal/handler/connect/`
    - `cd services/api && GOWORK=off go build ./internal/handler/connect/...` exits 0
    - `cd services/api && GOWORK=off go vet ./internal/handler/connect/...` exits 0
    - Each connect/*.go file ≤500 LOC
    - `rg -c '^func NewConnectHandler\(' services/api/internal/handler/connect/connect.go` returns 1
    - `rg -c '^type ConnectConfig struct' services/api/internal/handler/connect/connect.go` returns 1
    - `rg -c '^func \(h \*ConnectHandler\) (VerifyTelegramLogin|ConnectTelegram|RefreshTelegramLinkedGroup)\(' services/api/internal/handler/connect/telegram.go` returns ≥1
    - `rg -c '^func \(h \*ConnectHandler\) (ConnectVK|RefreshVKCommunityName)\(' services/api/internal/handler/connect/vk_community.go` returns ≥1
    - ConnectConfig has only the trimmed field set: `rg 'TelegramBotToken|VKServiceKey|FrontendURL' services/api/internal/handler/connect/connect.go | wc -l` returns ≥3
    - ConnectConfig does NOT include OAuth-only fields: `rg 'YandexClientID|GoogleClientID|VKClientID' services/api/internal/handler/connect/connect.go | wc -l` returns 0
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go build ./internal/handler/connect/...</automated>
</task>

<task type="auto">
  <id>19-04-03</id>
  <title>Update router.go + wire/handlers.go, delete legacy handler/oauth.go</title>
  <wave>2</wave>
  <read_first>
    - services/api/internal/router/router.go (current — Handlers struct + route table)
    - services/api/internal/wire/handlers.go (created by 19-01)
    - services/api/internal/handler/oauth.go (legacy file — slated for deletion)
    - services/api/internal/handler/oauth_test.go (verify private-helper invocations)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md (router.go updates lines 519-540)
  </read_first>
  <action>
    1. **Test-import grep** before changes: run `rg "h\.(probeVKCommunityToken|checkVKWallScope|telegramGetChat|probeTelegramLinkedGroup|resolveVKGroupID|fetchVKCommunityName|googleDiscoverAccounts|googleDiscoverLocations)" services/api/internal/handler/*_test.go` — record the private-helper invocations. If ANY test invokes a private helper that we're moving to a different receiver, apply D-16 enforcement rule #1: keep a facade method on the original receiver type. Document affected helpers in the commit message.

    2. **Update `services/api/internal/router/router.go`**:
       - Add `Connect *connect.ConnectHandler` to the `Handlers` struct (alongside existing `OAuth *oauth.OAuthHandler` — the import path changes from `handler.OAuthHandler` to `oauth.OAuthHandler`).
       - Update the type of `OAuth` field from `*handler.OAuthHandler` to `*oauth.OAuthHandler`.
       - Update route registrations for paste-flow routes only:
         ```go
         // BEFORE
         r.Post("/integrations/vk/connect",            handlers.OAuth.ConnectVK)
         r.Post("/integrations/vk/{id}/refresh-name",  handlers.OAuth.RefreshVKCommunityName)
         r.Post("/integrations/telegram/verify",       handlers.OAuth.VerifyTelegramLogin)
         r.Post("/integrations/telegram/connect",      handlers.OAuth.ConnectTelegram)
         r.Post("/integrations/telegram/refresh",      handlers.OAuth.RefreshTelegramLinkedGroup)

         // AFTER
         r.Post("/integrations/vk/connect",            handlers.Connect.ConnectVK)
         r.Post("/integrations/vk/{id}/refresh-name",  handlers.Connect.RefreshVKCommunityName)
         r.Post("/integrations/telegram/verify",       handlers.Connect.VerifyTelegramLogin)
         r.Post("/integrations/telegram/connect",      handlers.Connect.ConnectTelegram)
         r.Post("/integrations/telegram/refresh",      handlers.Connect.RefreshTelegramLinkedGroup)
         ```
       - **All `/oauth/*` routes (VKCallback, VKCommunityCallback, GetVKAuthURL, etc.) keep `handlers.OAuth.*` dispatch — UNCHANGED.**
       - The URL paths `/oauth/...` and `/integrations/...` are byte-identical before and after. Only the Go handler-name on the right side of the dispatch changes for the 5 paste-flow routes.

    3. **Update `services/api/internal/wire/handlers.go`** (created by 19-01):
       - Replace `oauthHandler := handler.NewOAuthHandler(...)` invocation with `oauthHandler := oauth.NewOAuthHandler(...)` (import path change).
       - Add `connectHandler := connect.NewConnectHandler(integrationService, businessService, connect.ConnectConfig{ TelegramBotToken: cfg.TelegramBotToken, VKServiceKey: cfg.VKServiceKey, FrontendURL: cfg.FrontendURL }, &http.Client{Timeout: 10*time.Second})`.
       - Populate `Handlers{ OAuth: oauthHandler, Connect: connectHandler, ... }`.

    4. **Delete `services/api/internal/handler/oauth.go`** — the legacy 1703-LOC file. Every method has been moved.

    5. **Update `services/api/internal/handler/oauth_test.go`** (D-16 import-path-only) — change the package import and constructor invocation:
       ```go
       // BEFORE
       h := handler.NewOAuthHandler(...)
       // AFTER
       h := oauth.NewOAuthHandler(...)
       ```
       If any test invokes a Telegram or VK paste-flow private helper that's now on `*ConnectHandler`, apply rule #1: keep a facade in `connect/connect.go` so the test stays unchanged. If the test was a true package-level test of an entry-point method (e.g., `TestOAuthHandler_VKCallback`), move the test file too: split into `services/api/internal/handler/oauth/oauth_test.go` and `services/api/internal/handler/connect/connect_test.go`. Per D-16: assertions stay byte-identical; only import paths and receiver-type references change.

    6. **Update any other consumer** of `handler.OAuthHandler` in `services/api/cmd/main.go` if not already cleaned up by 19-01. Most likely none — the wire/handlers.go owns all construction now.

    Commit subject: `refactor(19): wire handler/oauth + handler/connect, delete legacy oauth.go`.
  </action>
  <acceptance_criteria>
    - File `services/api/internal/handler/oauth.go` does NOT exist (deleted): `test ! -f services/api/internal/handler/oauth.go`
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0 (oauth_test.go and any siblings pass with import-path-only updates)
    - Public route URL paths unchanged: `rg '^\s*r\.(Get|Post)\("/oauth' services/api/internal/router/router.go | wc -l` matches the count in `git show main:services/api/internal/router/router.go | rg '^\s*r\.(Get|Post)\("/oauth' | wc -l`
    - Public integration paste-flow URL paths unchanged: `rg '^\s*r\.Post\("/integrations/(telegram|vk)/' services/api/internal/router/router.go | wc -l` matches main's count
    - 5 paste-flow routes dispatch to `handlers.Connect`: `rg 'handlers\.Connect\.' services/api/internal/router/router.go | wc -l` returns 5
    - OAuth code-flow routes still dispatch to `handlers.OAuth`: `rg 'handlers\.OAuth\.(GetVKAuthURL|VKCallback|GetYandexAuthURL|YandexCallback|GetGoogleAuthURL|GoogleCallback|GoogleLocations|GoogleSelectLocation)' services/api/internal/router/router.go | wc -l` returns 8
    - `git diff $(git merge-base HEAD main)..HEAD -- services/api/internal/handler/oauth_test.go services/api/internal/handler/oauth/*_test.go services/api/internal/handler/connect/*_test.go | rg '^\+\s+(assert\.|require\.|t\.Errorf|t\.Fatalf)' | wc -l` returns 0 (no new assertions per D-16)
    - `bash scripts/check-loc.sh` no longer flags any oauth/connect file (the only thing flagged among these dirs)
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go test -race ./... &amp;&amp; bash -c '! test -f services/api/internal/handler/oauth.go'</automated>
</task>

</tasks>

## Verification

```bash
# SC-01: oauth.go gone, no oauth/* or connect/* file >500 LOC
test ! -f services/api/internal/handler/oauth.go
wc -l services/api/internal/handler/oauth/*.go services/api/internal/handler/connect/*.go | awk '$2!="total" && $1>500 {print; exit 1}'

# SC-03: route paths unchanged (compare path strings only)
diff <(rg -o '"/(oauth|integrations)/[^"]*"' services/api/internal/router/router.go | sort -u) \
     <(git show main:services/api/internal/router/router.go | rg -o '"/(oauth|integrations)/[^"]*"' | sort -u)

# SC-02
make lint-all && make test-all
```

## Success Criteria

- `oauth.go` deleted; `handler/oauth/` and `handler/connect/` exist with each file ≤500 LOC
- All public OAuth + Connect URL paths byte-identical (SC-03)
- Existing oauth tests pass with import-path-only updates (SC-03 / D-16)
- `make lint-all && make test-all` green (SC-02)
