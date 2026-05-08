---
plan: 19-08
phase: 19
slug: yandex-pool-decompose
wave: 4
depends_on: [19-07]
files_modified:
  - services/agent-yandex-business/internal/yandex/pool.go
  - services/agent-yandex-business/internal/yandex/pool_test.go
files_created:
  - services/agent-yandex-business/internal/yandex/session.go
  - services/agent-yandex-business/internal/yandex/business_browser.go
  - services/agent-yandex-business/internal/yandex/tool_list_companies.go
  - services/agent-yandex-business/internal/yandex/tool_get_reviews.go
  - services/agent-yandex-business/internal/yandex/tool_reply_review.go
  - services/agent-yandex-business/internal/yandex/tool_get_info.go
  - services/agent-yandex-business/internal/yandex/tool_update_info.go
  - services/agent-yandex-business/internal/yandex/tool_update_hours.go
  - services/agent-yandex-business/internal/yandex/tool_create_post.go
  - services/agent-yandex-business/internal/yandex/tool_upload_photo.go
  - services/agent-yandex-business/internal/yandex/helpers.go
files_deleted: []
success_criteria: [SC-01, SC-03]
autonomous: true
estimated_loc_delta: -1100 / +1350
---

## Plan Goal

Decompose `services/agent-yandex-business/internal/yandex/pool.go` (1242 LOC, three layers fused: pool lifecycle + session/cookie management + 7 RPA tool methods) into:

- `pool.go` — `BrowserPool` lifecycle ONLY (lines 1-215 verbatim)
- `session.go` — cookie injection + OAuth-to-session exchange (lines 218-302)
- `business_browser.go` — `BusinessBrowser` struct + `ForBusiness` constructor + `baseURL()` (lines 304-326)
- `tool_*.go` per-tool files (8 files, one per RPA tool): `list_companies`, `get_reviews`, `reply_review`, `get_info`, `update_info`, `update_hours`, `create_post`, `upload_photo`
- `helpers.go` — shared free functions used cross-file (`debugScreenshot`, multi-consumer helpers)

**Layout decision (RESEARCH §6 R2 + §16 Q1):** Same package `yandex` (NOT a sub-package). Methods on `*BusinessBrowser` cannot live in a different package than the receiver type. The "per-tool files" interpretation of D-08 = files-per-tool, not packages-per-tool. Files use the prefix `tool_<name>.go` for clarity; CONTEXT.md D-08 wording about a `tools/` sub-folder is honoured in spirit (per-tool files) but as same-package files for Go method-receiver compatibility.

**CRITICAL: D-09 PRE-SPLIT TEST GATE.** Plan task 19-08-01 adds 15-18 Playwright-mocked unit tests for every `BusinessBrowser` method touched, BEFORE any decomposition commit. Tests exercise the un-split code first; the same suite must stay green after the split. This mitigates SPEC risk #3 ("Hidden coupling in pool.go").

Implements: D-08 (per-tool files), D-09 (pre-split tests), R2 (same-package layout).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/agent-yandex-business/internal/yandex/pool.go
@services/agent-yandex-business/internal/yandex/mock_page_test.go
@services/agent-yandex-business/internal/yandex/pool_test.go
@services/agent-yandex-business/AGENTS.md
</context>

<tasks>

<task type="auto">
  <id>19-08-01</id>
  <title>Add 15-18 Playwright-mocked pre-split tests pinning every BusinessBrowser method</title>
  <wave>1</wave>
  <read_first>
    - services/agent-yandex-business/internal/yandex/pool.go (full file — every BusinessBrowser method body)
    - services/agent-yandex-business/internal/yandex/mock_page_test.go (existing 153-LOC mockPage fixture)
    - services/agent-yandex-business/internal/yandex/pool_test.go (existing test style)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 6 "D-09 Pre-split test strategy" lines 790-827)
  </read_first>
  <action>
    Add ~17 new Playwright-mocked tests to existing `services/agent-yandex-business/internal/yandex/pool_test.go` (or split into `business_browser_test.go` if pool_test.go gets too long; tests are exempt from SC-01 anyway). Each test uses the existing `mockPage` from `mock_page_test.go`. **Tests must be added BEFORE the decomposition split.** Run them once on the un-split code to confirm they pass; this pins behaviour.

    Required tests (RESEARCH §6 D-09 list):

    | # | Test name | Pins behaviour |
    |---|---|---|
    | 1 | `TestBusinessBrowser_ListCompanies_ParsesCompanyRows` | `.CompaniesCompanyRow` selector + permalink regex `\/sprav\/(\d+)\/p\/edit` + JSON shape `[{permalink, name}]` |
    | 2 | `TestBusinessBrowser_ListCompanies_PassportRedirect_ReturnsNonRetryable` | `passport.yandex` URL → wrapped `ErrSessionExpired` |
    | 3 | `TestBusinessBrowser_ListCompanies_NoCompanies_ReturnsEmpty` | Locator timeout → empty slice, not error |
    | 4 | `TestBusinessBrowser_GetReviews_ScrapesNCards_LimitClamped` | limit ≤0 → 20, limit >50 → 50 |
    | 5 | `TestBusinessBrowser_GetReviews_ExtractsAuthorRatingText` | extractText fallback chain hits; extractRating returns int |
    | 6 | `TestScrapeReviewCards_HandlesEmpty` | Empty DOM → empty slice, no error |
    | 7 | `TestExtractText_FallbackChain` | First selector misses → tries next |
    | 8 | `TestExtractRating_NumericVsString` | Numeric returns int, missing returns nil |
    | 9 | `TestBusinessBrowser_ReplyReview_NavigatesAndClicksSave` | `navigateToEditPage` + `clickSave` call sequence |
    | 10 | `TestBusinessBrowser_GetInfo_ScrapesAllFields` | name/phone/email/hours/address/status all extracted |
    | 11 | `TestBusinessBrowser_UpdateInfo_FillsFormFields` | only set fields are typed |
    | 12 | `TestBusinessBrowser_UpdateHours_FormatsHoursPayload` | `formatHoursForYandex(json)` → expected schedule string |
    | 13 | `TestFormatHoursForYandex_Closed` | "closed" day → "Закрыто" |
    | 14 | `TestFormatHoursForYandex_OpenSlot` | `{open:"09:00",close:"21:00"}` → `09:00–21:00` |
    | 15 | `TestBusinessBrowser_CreatePost_SubmitsTextareaAndClicksPublish` | selector + click order |
    | 16 | `TestBusinessBrowser_UploadPhoto_SetsCategoryAndUploadsFile` | category radio + file input + publish click |
    | 17 | `TestClosePopups_ClicksKnownDismissSelectors` | known dismiss selectors clicked |

    Specific elements:
    - Each test follows existing `pool_test.go` style — match its assert idiom (bare `t.Errorf`/`t.Fatalf` if pool_test.go is in that style; testify `require/assert` if pool_test.go uses testify).
    - Tests use existing `newMockBrowserPool` helper if available; otherwise create one in the test file that constructs a `*BrowserPool` whose `WithPage` runs the callback against `mockPage` instead of real Playwright.
    - For free-function tests (Test #6, #7, #8, #13, #14): call the function directly; no mockPage construction needed.
    - For RPA-method tests (#1-5, #9-12, #15-17): construct a `BusinessBrowser` via `pool.ForBusiness(...)`, invoke the method, assert the observable outcome (return value, error type, selector calls captured by mockPage).

    **Run the suite at end of task — must be green BEFORE proceeding to 19-08-02.**

    Anti-pattern (RESEARCH §6 anti-pattern callout):
    - Do NOT decompose first and write tests after. The behaviour-preservation guarantee depends on tests passing BEFORE the split too.

    Commit subject: `test(19-08): add pre-split BusinessBrowser pinning tests for D-09 gate`.
  </action>
  <acceptance_criteria>
    - All ~17 test names from the table exist: `rg -c '^func TestBusinessBrowser_|^func TestFormatHoursForYandex_|^func TestScrapeReviewCards_|^func TestExtractText_|^func TestExtractRating_|^func TestClosePopups_' services/agent-yandex-business/internal/yandex/*_test.go` returns ≥17
    - `cd services/agent-yandex-business && GOWORK=off go test -race -run 'BusinessBrowser|FormatHoursForYandex|ScrapeReviewCards|ExtractText|ExtractRating|ClosePopups' ./internal/yandex/...` exits 0
    - `cd services/agent-yandex-business && GOWORK=off go test -race ./...` exits 0 (full suite green)
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/agent-yandex-business &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

<task type="auto">
  <id>19-08-02</id>
  <title>Split pool.go into per-layer + per-tool files (same package)</title>
  <wave>2</wave>
  <read_first>
    - services/agent-yandex-business/internal/yandex/pool.go (1242 LOC; full method-by-method line ranges)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 6 "Current shape" table lines 749-761; "Recommended layout" lines 862-877)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-08" lines 843-916)
  </read_first>
  <action>
    Decompose `pool.go` into 11 files. **Same Go package `yandex`** — methods on `*BusinessBrowser` stay on the same receiver, just spread across files. Move method bodies VERBATIM. No logic edits.

    Mapping (line ranges from current pool.go):

    | New file | Lifts from pool.go | Symbols |
    |---|---|---|
    | `pool.go` (kept) | 1-215 | `pooledContext`, `BrowserPool`, `NewBrowserPool*`, `ensureBrowser`, `getOrCreateContext`, `WithPage`, `EvictContext`, `evictLoop`, `Close` |
    | `session.go` (new) | 218-302 | `injectCookies`, `exchangeOAuthForSession`, `isOAuthToken` |
    | `business_browser.go` (new) | 304-336 | `BusinessBrowser` struct, `ForBusiness` constructor, `baseURL()` method |
    | `tool_list_companies.go` | 337-402 | `ListCompanies` |
    | `tool_get_reviews.go` | 405-625 | `GetReviews`, `scrapeReviewCards`, plus `extractText` / `extractRating` / `normalizeWhitespace` IF only used here (see helpers.go decision below) |
    | `tool_reply_review.go` | 627-774 | `ReplyReview`, `navigateToEditPage`, `clickSave` |
    | `tool_get_info.go` | 777-867 | `GetInfo` |
    | `tool_update_info.go` | 869-929 | `UpdateInfo` |
    | `tool_update_hours.go` | 931-1095 | `UpdateHours`, `closePopups` (if only used here), `formatHoursForYandex` |
    | `tool_create_post.go` | 1097-1158 | `CreatePost` |
    | `tool_upload_photo.go` | 1160-1242 | `UploadPhoto` |
    | `helpers.go` (new) | (free functions used by ≥2 tool files) | `debugScreenshot`, multi-consumer helpers |

    **Helper-placement decision rule:** Before placing `extractText`, `extractRating`, `normalizeWhitespace`, `closePopups`, `debugScreenshot` — run:

    ```bash
    rg 'extractText\(|extractRating\(|normalizeWhitespace\(|closePopups\(|debugScreenshot\(' services/agent-yandex-business/internal/yandex/ --type go
    ```

    - If a helper is called from ≥2 tool methods → place in `helpers.go`
    - If a helper is called from exactly 1 tool method → place collocated with that method's file

    Specific rules:
    1. Every file declares `package yandex`.
    2. Method receiver `*BusinessBrowser` unchanged across all tool files.
    3. Method bodies copied byte-for-byte. No logic edits.
    4. Imports per file: stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` (no service-internal imports needed since same package).
    5. Pre-split tests from task 19-08-01 stay in their existing file(s) — symbols are package-level so they still see `BusinessBrowser`, `extractText`, etc.
    6. After move, `pool.go` should be ~215 LOC. Every tool_*.go file ≤500 LOC. helpers.go ≤500 LOC. session.go ≤500 LOC.

    Anti-pattern enforcement (RESEARCH §6):
    - Do NOT change `isOAuthToken` semantics — `strings.HasPrefix` check verbatim.
    - Do NOT introduce a sub-package `tools/` — Go method-receiver constraint blocks this.
    - Do NOT change `WithPage`'s mutex serialization — preserved as long as methods stay on `*BusinessBrowser` and call `bb.pool.WithPage`.

    Commit subject: `refactor(19): decompose yandex pool.go into pool/session/business_browser/tool_* files`.
  </action>
  <acceptance_criteria>
    - All 11 new/kept files exist under `services/agent-yandex-business/internal/yandex/`
    - `cd services/agent-yandex-business && GOWORK=off go build ./...` exits 0
    - `cd services/agent-yandex-business && GOWORK=off go test -race ./...` exits 0 (pre-split tests still pass)
    - `wc -l services/agent-yandex-business/internal/yandex/pool.go | awk '{exit ($1<=300)?0:1}'` (lifecycle-only, ~215 LOC target)
    - Every yandex/*.go (excluding test files) ≤500 LOC
    - `BusinessBrowser` methods live across new tool files: `rg -c '^func \(bb \*BusinessBrowser\) (ListCompanies|GetReviews|ReplyReview|GetInfo|UpdateInfo|UpdateHours|CreatePost|UploadPhoto)\(' services/agent-yandex-business/internal/yandex/tool_*.go` returns 8 across files
    - All files declare `package yandex`: `rg -L '^package yandex$' services/agent-yandex-business/internal/yandex/ -g '*.go' -g '!*_test.go'` returns 0 lines
    - `bash scripts/check-loc.sh` no longer flags `services/agent-yandex-business/internal/yandex/pool.go`
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/agent-yandex-business &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

</tasks>

## Verification

```bash
# SC-01: pool.go small + every other file ≤500 LOC
test "$(wc -l < services/agent-yandex-business/internal/yandex/pool.go)" -le 300
wc -l services/agent-yandex-business/internal/yandex/*.go | grep -v _test | awk '$2!="total" && $1>500 {print; exit 1}'

# SC-03: all tests still pass
cd services/agent-yandex-business && GOWORK=off go test -race ./...

# Pre-split tests integrity
rg -c '^func (TestBusinessBrowser_|TestFormatHoursForYandex_|TestScrapeReviewCards_|TestExtractText_|TestExtractRating_|TestClosePopups_)' services/agent-yandex-business/internal/yandex/*_test.go

# SC-02
make lint-all && make test-all
```

## Success Criteria

- `pool.go` reduced to ~215 LOC (lifecycle only)
- 7+ per-tool files each ≤500 LOC
- 17+ new Playwright-mocked pinning tests added BEFORE the split (D-09 gate)
- All tests (pre-existing + new) pass after the split (SC-03 — assertions unchanged)
- `make lint-all && make test-all` green (SC-02)
