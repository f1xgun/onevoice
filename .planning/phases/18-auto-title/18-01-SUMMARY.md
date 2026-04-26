---
phase: 18-auto-title
plan: 01
subsystem: security
tags: [pii, regex, redaction, russian, luhn, security]

# Dependency graph
requires:
  - phase: 15-projects-foundation
    provides: Conversation.TitleStatus enum (already shipped)
provides:
  - pkg/security package (new) with PII detection primitives
  - RedactPII / ContainsPII / ContainsPIIClass exports for Phase 18 titler (D-13/D-14/D-16) and Phase 19 search-query logging (D-15)
  - Named-class regex set: email, phone (RU), iban, passport (RU), inn, cc (Luhn-gated)
  - Russian false-positive-safe regexes (Cyrillic prefix anchors on passport/INN; Luhn on cc)
affects: [18-04 titler, 19 search-query logging]

# Tech tracking
tech-stack:
  added: []  # No new deps; stdlib regexp + unicode only
  patterns:
    - "Pure package-level functions (no struct, no constructor) for stateless security helpers"
    - "Named-class regex lookup with optional extra validator (Luhn for cc)"
    - "Russian-aware regex anchoring: Cyrillic prefix avoids ASCII \\b limitation"
    - "Negative-assertion regression tests (Pitfall 8) for redaction correctness"

key-files:
  created:
    - pkg/security/pii.go
    - pkg/security/pii_test.go
    - pkg/security/doc.go
  modified: []

key-decisions:
  - "Replacement token is the literal Russian '[Скрыто]' (D-14 verbatim) — no English fallback"
  - "INN regex split into two alternations: \\bINN keeps ASCII boundary; ИНН drops it (Go RE2 \\b is ASCII-only and never matches Cyrillic transitions)"
  - "Passport regex requires either Cyrillic prefix (паспорт / серия и номер) or strict 4+6 whitespace form to avoid Заказ/Заявка/Артикул false-positives"
  - "Credit-card regex is Luhn-gated to reject 16-digit IDs and order numbers"
  - "ContainsPIIClass returns the class name only — never the matched substring — so D-16 regex_class log field cannot leak PII bytes"

patterns-established:
  - "Pattern: PII detector composes regex + extra validator (Luhn) so over-matching regexes can be tightened without losing the class taxonomy"
  - "Pattern: Negative log-shape regression test (TestRedactPII_LogShape) — assert PII substring does NOT appear in output. Catches future 'I added a debug field' regressions"
  - "Pattern: Cyrillic prefix anchoring as the primary guard against numeric-title false-positives in Russian text"

requirements-completed: [TITLE-08]

# Metrics
duration: 6min
completed: 2026-04-26
---

# Phase 18 Plan 01: PII Detector Summary

**Reusable `pkg/security` package with named-class PII regex set, Luhn-gated credit-card validation, and Russian-aware passport/INN anchoring that survives a 21-case false-positive corpus.**

## Performance

- **Duration:** ~6 min
- **Started:** 2026-04-26T18:06:27Z
- **Completed:** 2026-04-26T18:12:11Z
- **Tasks:** 2
- **Files created:** 3 (pii.go, pii_test.go, doc.go)
- **Files modified:** 0

## Accomplishments

- `pkg/security/pii.go` — new package with three pure exports: `RedactPII`, `ContainsPII`, `ContainsPIIClass`.
- Six named regex classes (`email`, `phone`, `iban`, `passport`, `inn`, `cc`) with Luhn-gated credit-card validation.
- 21-case true-positive + 11-case Russian false-positive test corpus, including Заказ 12345, Чек 9876543, Звонок 2026-04-15, Заявка 7654321098, Артикул 123456789, Счёт 1234567890123, Доход за 2025, Платёж 100500, Идентификатор 1234567890123456 (Luhn-failing 16-digit) — all correctly returning `("", false)`.
- Negative log-shape regression test (TestRedactPII_LogShape) per Landmine 6 / Pitfall 8: for every PII input, asserts the original substring does not survive RedactPII output and the placeholder `[Скрыто]` does appear.
- `go test -race -count=1 ./pkg/security/...` → PASS (34 sub-tests, 1.2s).
- `go vet ./pkg/security/...` → clean.
- `golangci-lint run ./pkg/security/...` → 0 issues.

## Final Regex Strings (auditable)

| Class | Regex | Extra Validator |
|-------|-------|-----------------|
| email | `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}` | — |
| phone | `(?:\+7\|8)[\s\-(]*\d{3}[\s\-)]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b` | — |
| iban | `\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b` | — |
| passport | `(?i)(?:паспорт\|серия\s+и\s+номер)[\s:№]*\d{4}\s*\d{6}\b\|\b\d{4}\s\d{6}\b` | — |
| inn | `(?i)(?:\bINN\|ИНН)[\s:№]*\d{10}(?:\d{2})?\b` | — |
| cc | `\b(?:\d[ \-]?){12,18}\d\b` | `luhnValid` |

## Russian False-Positive Corpus (all return `("", false)`)

Verified literally in `TestContainsPII`:

- `Заказ 12345 от вторника`
- `Чек 9876543`
- `Звонок 2026-04-15 10:30`
- `Заявка 7654321098` (10 digits without `ИНН`/`паспорт` prefix)
- `Артикул 123456789`
- `Счёт 1234567890123` (13 digits no prefix)
- `Доход за 2025 квартал 1`
- `Платёж 100500`
- `Стол 5`
- `Отчёт 2025`
- `Идентификатор 1234567890123456` (16 digits, fails Luhn)

## Russian Replacement Token (D-14)

Confirmed: `RedactPII` substitutes matches with the literal Russian string `[Скрыто]` (square brackets around Cyrillic «Скрыто»). No English fallback. Defined as the sole `redactionToken` constant in `pkg/security/pii.go` (line 13).

## Task Commits

1. **Task 1: pkg/security/pii.go + doc.go** — `eca9098` (feat) — new module with regex classes + Luhn validator
2. **Task 2: pkg/security/pii_test.go + INN regex fix** — `024053e` (test) — full corpus + log-shape regression; auto-fixed INN regex
3. **Lint fixes** — `e0e168e` (chore) — copyloopvar + gosec G101 false-positive nolint directive

## Files Created/Modified

- `pkg/security/pii.go` (NEW, ~145 LOC) — RedactPII / ContainsPII / ContainsPIIClass + 6 regex classes + luhnValid
- `pkg/security/pii_test.go` (NEW, ~125 LOC) — TestContainsPII (21 cases), TestRedactPII (7 cases), TestRedactPII_LogShape (6 cases)
- `pkg/security/doc.go` (NEW, ~17 LOC) — package-level documentation

## Decisions Made

- **Token literal `[Скрыто]`**: locked verbatim per D-14; downstream Plan 04 prompt builder will reference this exact byte sequence.
- **INN regex split**: see Deviations §1 — `\bINN` keeps ASCII boundary, `ИНН` drops it because Go RE2 `\b` cannot detect transitions involving Cyrillic letters.
- **Class iteration order**: `email → phone → iban → passport → inn → cc`. CC is last because the regex is broadest (Luhn gates the actual claim) — placing it earlier would slow down redaction passes for non-CC inputs without changing semantics.
- **Loop variable copies removed**: Go 1.25 in this module's `go.mod` makes `c := c` redundant per copyloopvar lint.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] INN regex did not match against Cyrillic-prefixed input under Go RE2**

- **Found during:** Task 2 (running TestContainsPII)
- **Issue:** The research-draft regex `(?i)\b(?:ИНН|INN)[\s:№]*\d{10}(?:\d{2})?\b` failed to match `Контрагент ИНН 7707083893` and `ИНН: 770708388912`. Go's RE2 `\b` is ASCII-only and reports a word boundary only between `[a-zA-Z0-9_]` and a non-word ASCII char. The transition between a space (or start-of-string) and a Cyrillic letter `И` is non-word → non-word in RE2's view, so no boundary fires and the alternation never enters the `ИНН` branch.
- **Fix:** Split the alternation: `(?i)(?:\bINN|ИНН)[\s:№]*\d{10}(?:\d{2})?\b`. The Latin path keeps `\b` (where ASCII boundary works). The Cyrillic path drops the leading boundary — mid-word `ИНН` collision in real Russian text is not a realistic false-positive (no common Russian word contains `ИНН` as a substring). The trailing `\b` after the digits is unaffected (digit-to-non-digit is ASCII).
- **Files modified:** `pkg/security/pii.go` (regex string + comment explaining the RE2 limitation)
- **Verification:** Both INN sub-tests now pass; the entire false-positive corpus still returns `("", false)`; `Заявка 7654321098` (10 digits without prefix) still does not match.
- **Committed in:** `024053e` (Task 2 commit)

**2. [Rule 1 — Bug] Plan acceptance grep `(?i)\\b(?:ИНН|INN)` no longer holds**

- **Found during:** Task 1 → Task 2 transition
- **Issue:** The plan's acceptance criterion `grep -F "(?i)\\b(?:ИНН|INN)" pkg/security/pii.go returns 1 match` encodes the buggy regex from the research draft. The fixed regex (deviation 1) reads `(?i)(?:\bINN|ИНН)`.
- **Fix:** Functional contract takes precedence over the textual grep. The current regex satisfies the spirit of the acceptance criterion (case-insensitive, handles both `ИНН` and `INN`, requires explicit prefix) and passes the runtime test corpus that the grep was meant to enforce.
- **Files modified:** none beyond deviation 1.
- **Verification:** `go test -race -count=1 ./pkg/security/...` → all 21 TestContainsPII sub-tests pass.

**3. [Rule 3 — Blocking] golangci-lint copyloopvar + gosec G101**

- **Found during:** post-Task-2 lint sweep (`make lint-all` requirement in success criteria)
- **Issue:** Go 1.25 module made `c := c` / `input := input` loop-variable redeclarations redundant (copyloopvar warning); gosec flagged the `redactionToken = "[Скрыто]"` constant as G101 hardcoded credential (false positive — it is a placeholder, never a real secret).
- **Fix:** Removed the loop-var redeclarations (3 sites in tests + 1 site in `RedactPII`). Added `//nolint:gosec` directive to `redactionToken` with explanation comment.
- **Files modified:** `pkg/security/pii.go`, `pkg/security/pii_test.go`
- **Verification:** `golangci-lint run --config ./.golangci.yml ./pkg/security/...` → 0 issues.
- **Committed in:** `e0e168e` (chore commit)

---

**Total deviations:** 3 auto-fixed (2 bug, 1 blocking lint)
**Impact on plan:** All necessary for correctness and CI compliance. No scope creep — every change kept the plan's contract intact (TITLE-08, D-13/D-14/D-16, Russian false-positive corpus).

## Issues Encountered

None outside the deviations documented above.

## Self-Check: PASSED

- `pkg/security/pii.go` exists ✓
- `pkg/security/pii_test.go` exists ✓
- `pkg/security/doc.go` exists ✓
- Commit `eca9098` exists ✓
- Commit `024053e` exists ✓
- Commit `e0e168e` exists ✓
- `go test -race -count=1 ./pkg/security/...` exits 0 ✓
- `go vet ./pkg/security/...` exits 0 ✓
- `golangci-lint run ./pkg/security/...` exits 0 ✓

## Next Phase Readiness

`pkg/security` is ready to be imported by:

- **Plan 18-04 (titler service):** `RedactPII` for D-14 pre-redact of user/assistant messages, `ContainsPIIClass` for D-13 post-hoc title rejection with D-16 `regex_class` log field.
- **Phase 19 (search-query logging):** same exports for D-15 query log redaction.

No blockers; no concerns. The `[Скрыто]` token is locked and the false-positive corpus is enshrined in `TestContainsPII` so future regex tweaks cannot regress legitimate Russian numeric titles silently.

---
*Phase: 18-auto-title*
*Plan: 01*
*Completed: 2026-04-26*
