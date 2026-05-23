# ADR-0001 — Keep parallel RU/EN renderers in `prompt/builder.go`

- **Status:** Accepted
- **Date:** 2026-05-23
- **Scope:** `services/orchestrator/internal/prompt/`

## Context

`services/orchestrator/internal/prompt/builder.go` renders the LLM system
prompt in two locales: Russian (default / pre-i18n baseline) and English.
The current shape is two near-mirror functions:

- `buildSystemContentRu(ctx)` ≈ 50 LOC — header, business fields, tone,
  date, integrations list, 5 rules.
- `buildSystemContentEn(ctx)` ≈ 50 LOC — every section above with the
  same structure and an English translation.

Plus two paired helpers (`restrictionsAllDisabled`,
`restrictionsAllowedOnly`) that each carry a RU and EN literal block,
≈ 15 LOC per locale per helper.

In total ≈ 260 LOC of parallel localized text. An architectural review
(`/improve-codebase-architecture`, v2 — 2026-05-23) flagged this as a
**Speculative** "Prompt Template Renderer" deepening opportunity. The
reviewer's candidate refactor extracted a `labels` struct (≈ 22 fields ×
N locales) plus one `render(ctx, lab labels)` ≈ 50 LOC function — saving
≈ 50 LOC and enforcing locale parity at the type level (an unset struct
field is a compile error rather than a forgotten translation).

The trade-off:

|                | **Today: parallel renderers**     | **Refactor: label table**         |
| -------------- | --------------------------------- | --------------------------------- |
| LOC            | ~260 (RU + EN)                    | ~210 (one renderer + N tables)    |
| Readability    | Read top-to-bottom in one language; structure visible inline | Renderer is generic; structure split across renderer + table |
| Parity         | Comment-enforced ("keep in lock-step"); breakable if a rule is added to one but not the other | Type-enforced (struct field must exist for every locale) |
| Adding a 3rd locale | Duplicate the ~50 LOC renderer | Add one `labels{...}` table |
| Drift risk     | Real but bounded — covered by `Contains(...)` substring assertions in builder_test.go | Lower — the type system prevents missing fields |

## Decision

**Keep the parallel RU/EN renderers as-is.** Do not extract a label table.

The reasoning is rooted in two principles from the architecture-review
methodology this repo uses:

1. **Two adapters = real seam.** Today, exactly two locales exist:
   Russian (the default the platform was built around) and English (added
   in the i18n-readiness phase). The refactor's win — enforced parity
   plus easier locale addition — pays off only when a third locale is on
   the roadmap. There is no current roadmap item, RFC, or business
   commitment for a third locale. Refactoring to a label table now is a
   speculative seam that doubles the cost of a future "remove i18n entirely
   if EN never lands" rollback path.

2. **Parallel localized text is the direct cost of supporting N locales**,
   not a shallow abstraction. The duplication is **content**, not
   structure. The renderer's structure (header → fields → tone → date →
   integrations → rules → optional restrictions → optional project block)
   is already concentrated in one place; only the words swap between
   functions. Extracting the words into a table moves them from one source
   file to a second one without changing the structural surface area.

## Alternatives considered

### Label table (refactor candidate)

```go
type labels struct {
    introLine, sectionBusiness, fieldCategory, ... string
}
var labelsRu = labels{...}
var labelsEn = labels{...}

func render(ctx BusinessContext, lab labels) string { ... }
```

**Rejected** because:

- Only 2 locales today, both stable. The two-adapters rule (a seam
  earns its keep only when ≥ 2 implementations exist) passes weakly: it
  is true today but the refactor's primary benefit (easier 3rd-locale
  addition) is not banked.
- Indirection cost: every label lookup becomes `lab.fieldCategory`
  instead of a literal at the use site, which makes reading a single
  locale's output less direct.
- The 50-LOC saving is real but small.
- Tests are `Contains(...)` substring-based — they would survive a
  table refactor with minimal churn, so test cost is not a blocker, but
  also not a justification.

### `text/template` external templates

**Rejected** as overkill. The renderer is ≈ 100 LOC including the
optional project block. A template engine adds parse-time errors and a
template-language learning curve in exchange for moving the same
structural shape into a template file. No win.

### SKIP without ADR

**Rejected** because without this record the same architectural review
will keep proposing the label-table refactor on every future sweep. ADRs
exist precisely to record load-bearing skip decisions.

## Consequences

**Maintenance cost.** Every new rule or field added to the system prompt
must be added to both `buildSystemContentRu` and `buildSystemContentEn`.
The package-level comment already calls this out; tests catch the
asymmetric-output case via `Contains(...)` assertions per locale.

**Locale parity is comment-enforced, not type-enforced.** Reviewers must
read both functions when changing one. This is acceptable while N=2.

**Reconsider when:**

- A third locale lands on a real roadmap (PRD / phase plan).
- The number of rules in the system prompt grows past ~10 (the current
  count is 5 — the cognitive cost of paired editing is still trivial).
- A test gap surfaces where one locale silently drifts from the other
  for a non-cosmetic field.

At any of those triggers, this ADR should be revisited and likely
superseded by an ADR-0002 that records adopting the label-table seam.
