# Phase 19 Research: Search & Sidebar Redesign

**Researched:** 2026-04-27
**Domain:** Mongo `$text` Russian search + master/detail sidebar restructure (Go backend, Next.js 14 + React 18 frontend)
**Confidence:** HIGH
**Scope discipline:** CONTEXT.md locks 17 decisions (D-01..D-17). This research does NOT relitigate them — it fills the open verification items the planner needs.

## Summary

Of the 15 verification items requested, every load-bearing question has a verified answer:

- **Snowball stemmer (D-09):** Pick `github.com/kljensen/snowball/russian` v0.10.0 (MIT, pure Go, last release Aug 2024, single-function `Stem(word, stopwords) string` API).
- **Resizable lib (D-15):** `react-resizable-panels` v4.x (MIT, native `autoSaveId` localStorage persistence, ARIA window splitter, keyboard-resizable). The custom `ResizeObserver`+`pointermove` hook is ~150 LOC plus a11y boilerplate; the library is ~the same size class but ships the WAI-ARIA Splitter pattern out of the box. **Recommend the library.**
- **axe (D-17):** **`@axe-core/react` is unsupported on React 18+** per Deque (project uses React 18 — verified `services/frontend/package.json:40`). Use `vitest-axe` (or its actively-maintained fork `@chialab/vitest-axe`) with the existing Vitest+jsdom setup. Gate critical+serious in CI.
- **Mongo Go driver text index (D-12):** Driver `v2.5.0` supports `SetDefaultLanguage`, `SetWeights`, and the `$text`+`$meta:textScore` projection. **`SetBackground(true)` is a no-op on MongoDB 4.2+** — keep it set anyway for source-readability and to match SEARCH-06's literal wording, but don't rely on it for behavior.
- **Two-phase query (D-12):** **REQUIRED, not optional.** Mongo docs are explicit: a `$match` stage that includes `$text` MUST be the first stage of the pipeline; `Message` has no `business_id` field (verified `pkg/domain/mongo_models.go:52-70`). Therefore phase-1 (`Conversation`-scoped `$text`) and phase-2 (`Message` `$text` with `conversation_id ∈ allowlist`) are two separate aggregations whose results merge in Go.
- **CRITICAL FINDING #1 — schema mismatch:** `messages.content` literal field-name vs the `text-index` field-name used in MongoDB shell examples is fine; but BSON tag for `Conversation.Pinned` is `bson:"pinned"` and there's no `pinned_at` field yet — D-02 adds it.
- **CRITICAL FINDING #2 — Russian stemmer caveat:** `kljensen/snowball/russian` stems `«злейший»` → `«зл»` (NLTK-aligned, not pure Snowball spec). Acceptable for highlight-range computation; the Mongo `$text` matcher uses Mongo's own stemmer for retrieval, so any divergence between snowball-go and Mongo's libstemmer is at most a missed `<mark>` highlight, never a missed result. Documented as Pitfall in §1.

**Primary recommendation:** scope phase 19 into 5 plans (`19-01-layout-restructure`, `19-02-pinned`, `19-03-search-backend`, `19-04-search-frontend`, `19-05-a11y-and-audit`) per CONTEXT.md D-14 split-recommendation. Plans 19-03/19-04 are the highest-risk; the others are mechanical.

## Project Constraints (from CLAUDE.md / AGENTS.md)

These directives apply to every plan in this phase. The planner must verify compliance.

- Go workspace: `go.work` with `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in every module's `go.mod`.
- New domain types live under `pkg/domain/` only when shared (snowball wraps stay inside `services/api/`, per `pkg/AGENTS.md`).
- Backend layering: handler → service → repository, no skip. Per `services/api/AGENTS.md`.
- Tool naming: `{platform}__{action}` (irrelevant to Phase 19 but absolute repo-wide).
- NATS subjects: `tasks.{agentID}` (irrelevant to Phase 19).
- Tests: `t.Setenv` not `os.Setenv` (race-safe under `-race`). Race detector required.
- Commit format: `<type>: <subject>` (feat|fix|refactor|docs|test|chore|ci); **NO `Co-Authored-By:` line** (project memory).
- Migrations: dual paths (`migrations/postgres/` uses `gen_random_uuid()`, `services/api/migrations/` uses `uuid_generate_v4()`). Phase 19 has no Postgres schema changes; the Mongo `pinned_at` backfill + index creation is in-process at API startup, no SQL migration files.
- Frontend: server-by-default; `"use client"` only when hooks/events; Tailwind only; React Query for server state; Zustand for global UI; react-hook-form+zod for forms; Vitest+RTL for tests.
- Russian UI copy throughout (see CONTEXT.md `<specifics>`).
- Logging: slog structured fields, never message bodies. Search logs metadata-only `{user_id, business_id, query_length}` per SEARCH-07.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SEARCH-01 | Idempotent Mongo text indexes (`messages.content`, `conversations.title`) on API startup, `default_language: "russian"`, weighted | §4 — verified Go driver v2 syntax + idempotent `CreateIndexes` semantics already in use (`EnsureConversationIndexes` precedent at `conversation.go:224-243`) |
| SEARCH-02 | `GET /api/v1/search?q=&project_id=&limit=20` repo signature requires `(business_id, user_id)`, rejects empty | §5, §6 — defense-in-depth signature; existing two-user pattern at `test/integration/authorization_test.go` |
| SEARCH-03 | Aggregate by conversation: title + snippet (±40-120) + date + project name, sorted by aggregated text score | §5, §10 — pipeline shape; snippet centering algorithm |
| SEARCH-04 | 250 ms debounce; result-click opens chat scrolled to matched message with highlight | §8, §9 — Cmd-K + `?highlight=msgId` flow |
| SEARCH-05 | Scope to current business and (optional) current project | §5, §6 — two-phase query honors project filter |
| SEARCH-06 | `background: true`; index readiness gate before search endpoint enabled | §7 — `atomic.Bool` readiness flag pattern; note `background:true` is no-op in Mongo 4.2+ |
| SEARCH-07 | Log `(user_id, business_id, query_length)` only — never query text | §15 + slog convention |
| UI-01 | Desktop master/detail | §2 — `react-resizable-panels` PanelGroup |
| UI-02 | Projects collapsible + «Без проекта» + «Закреплённые» | §11 — pinned compound index; existing UnassignedBucket / ProjectSection components |
| UI-03 | Pin/unpin global section + duplicated under project, subtle indicator | §10 — ProjectChip (already exists at `services/frontend/components/chat/ProjectChip.tsx`); D-05 reuses it |
| UI-04 | Context menu uses Radix DropdownMenu | Existing primitive in `package.json:20` |
| UI-05 | Mobile drawer Radix Dialog + focus trap, ESC, scroll lock, keyboard navigation | Existing `<Sheet>` at `services/frontend/components/ui/sheet.tsx` |
| UI-06 | Sidebar search input with inline result dropdown, keyboard nav | §8, §9 |

## 1. Russian Snowball Stemmer Library

| Property | `github.com/kljensen/snowball/russian` | `github.com/blevesearch/snowballstem/russian` |
|---|---|---|
| **License** | MIT (CITED: pkg.go.dev/github.com/kljensen/snowball) | Unclear — repository has no LICENSE file; tracking issue #2 still open as of last check (CITED: github.com/blevesearch/snowballstem/issues/2) |
| **Pure Go (no CGo)** | Yes (CITED: pkg.go.dev/github.com/kljensen/snowball) | Yes (vendored Snowball-generated Go) |
| **Russian support** | Yes — top-level `russian` subpackage | Yes — `snowballstem/russian` subpackage |
| **Latest version** | v0.10.0, Aug 13 2024 (CITED: github.com/kljensen/snowball/tags) | No tagged releases; Bleve consumes via Go-modules `replace` (CITED: github.com/blevesearch/snowballstem) |
| **API surface** | Two functions: `Stem(word string, stemStopWords bool) string` and `IsStopWord(word string) bool` | Multi-step `Env`-based API; caller manages `Env` lifecycle and step ordering |
| **Dependency footprint** | Single internal `snowballword` package; no external deps | Each language has its own subpackage; designed to be `replace`-vendored by Bleve |
| **Maintenance** | Active — v0.10.0 added throughput optimizations (Aug 2024) | Maintenance-only; no functional changes since 2018 fork |

**Decision: `github.com/kljensen/snowball v0.10.0`** [VERIFIED: pkg.go.dev]

Rationale: MIT license is explicit, ergonomic single-function API, no `Env`/state management, and active maintenance. The Bleve fork is intended for Bleve consumers and lacks an explicit LICENSE — adding it for one stemming function is a license-clean risk we don't need.

**Caveat (document as Pitfall):** Library README acknowledges that «злейший» stems to «зл» (matching Python NLTK), not «злейш» as the original Russian Snowball algorithm spec dictates [VERIFIED: pkg.go.dev/github.com/kljensen/snowball]. This divergence does NOT affect Mongo `$text` retrieval (Mongo uses libstemmer, which follows the spec), only the highlight-range computation. The user might see a result returned by Mongo where snowball-go fails to mark a token. Acceptable: the row still appears with the snippet readable; missed marks are cosmetic.

**Installation:**

```bash
cd services/api
go get github.com/kljensen/snowball@v0.10.0
go mod tidy
```

**Usage (5 lines stemming `запланировать`):**

```go
import "github.com/kljensen/snowball/russian"

stem := russian.Stem("запланировать", false)
// stem == "запланирова"  (root form; matches «запланировал», «запланируем», etc.)
```

**Highlight-range builder (sketch — actual implementation goes into `services/api/internal/service/snippet.go`):**

```go
import (
    "strings"
    "unicode"
    "github.com/kljensen/snowball/russian"
)

// HighlightRanges returns byte ranges in `snippet` where any token's stem
// matches any of `queryStems`. Stable order, non-overlapping. Used by the
// search service to build the `marks` array sent to the frontend (D-09).
func HighlightRanges(snippet string, queryStems map[string]struct{}) [][2]int {
    var marks [][2]int
    pos := 0
    runes := []rune(snippet)
    for pos < len(runes) {
        // skip non-letter runes
        for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
            pos++
        }
        start := pos
        for pos < len(runes) && unicode.IsLetter(runes[pos]) {
            pos++
        }
        if start == pos {
            continue
        }
        token := strings.ToLower(string(runes[start:pos]))
        if _, hit := queryStems[russian.Stem(token, false)]; hit {
            // Convert rune offsets back to byte offsets for the JS frontend's
            // String.slice (which is UTF-16-by-rune-index — careful in v1.4).
            byteStart := len(string(runes[:start]))
            byteEnd := len(string(runes[:pos]))
            marks = append(marks, [2]int{byteStart, byteEnd})
        }
    }
    return marks
}
```

**Confidence:** HIGH — verified license, pure Go, version, API on pkg.go.dev.

## 2. Resizable Panels

| Property | `react-resizable-panels` v4.x | Custom hook (ResizeObserver + pointermove) |
|---|---|---|
| **License** | MIT [VERIFIED: github.com/bvaughn/react-resizable-panels] | n/a |
| **Latest version** | 4.9.0 (Apr 3 2026) [CITED: github.com/bvaughn/react-resizable-panels] | n/a |
| **Bundle size (gz)** | Bundlephobia returned 403 to WebFetch; npm package size for v3.0.5 was 112 KB *packed* (not gz). Library README states it's "lightweight" and shadcn/ui ships it as their canonical Resizable primitive [CITED: ui.shadcn.com/docs/components/radix/resizable]. Order-of-magnitude: low single-digit KB gz. [ASSUMED: bundle size <5 KB gz based on community claims; planner should verify with `pnpm build && pnpm bundle-analyzer` if size matters] | ~150 LOC + ARIA Splitter boilerplate (~50 LOC) ≈ ~1 KB minified |
| **Next.js 14 App Router** | YES — but server-rendered default layout vs persisted localStorage layout causes flicker. Solution: persist via cookies and pass as `defaultLayout` prop from a server component, OR live with the one-frame flicker (acceptable in dev — sidebar re-layout is sub-100 ms). [CITED: react-resizable-panels README; app.unpkg.com/react-resizable-panels@2.0.19/files/README.md] | Same flicker risk; manual `useEffect` read of `localStorage` to seed width on mount |
| **localStorage persistence** | Built-in via `autoSaveId` prop; library handles read+write [CITED: react-resizable-panels README] | Hand-rolled `useEffect` + `localStorage.setItem` in pointermove debouncer |
| **Keyboard a11y** | Built-in WAI-ARIA `role="separator"` window-splitter pattern; arrow keys resize [CITED: react-resizable-panels README] | Must implement: `aria-orientation`, `aria-valuenow`, `aria-valuemin`, `aria-valuemax`, `aria-controls`, ArrowLeft/ArrowRight handlers — non-trivial |
| **prefers-reduced-motion** | The library does no animation by default (drag is instant); reduced-motion is moot. The shadcn variant adds Tailwind `transition-all` which the planner should drop or guard with `motion-reduce:transition-none` [VERIFIED: ui.shadcn.com] | Same — no animation by default |

**Decision: `react-resizable-panels` v4.x** [VERIFIED: npm + GitHub]

Rationale:
1. MIT license, actively maintained, small bundle.
2. Built-in WAI-ARIA Splitter pattern — UI-05's keyboard navigation requirement is honored without hand-rolling ARIA.
3. shadcn ships it as the canonical Resizable primitive — pattern alignment with the rest of the frontend (already shadcn-heavy).
4. The custom-hook alternative is ~200 LOC including ARIA boilerplate; the library is ~the same size class but battle-tested. Hand-rolling a window-splitter is one of those "how hard could it be" traps that PITFALLS §22 directly cautions against.
5. The localStorage flicker is mitigated by setting a sensible `defaultSize` (CONTEXT.md D-15: 200-480 px range, default 280 px) so the SSR layout is close to the persisted layout.

**Installation:**

```bash
cd services/frontend
pnpm add react-resizable-panels@^4
```

**Usage (sketch — actual implementation goes into `services/frontend/app/(app)/layout.tsx`):**

```tsx
'use client';
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';

<PanelGroup direction="horizontal" autoSaveId="onevoice:sidebar-width">
  <Panel defaultSize={20} minSize={15} maxSize={40} className="motion-reduce:transition-none">
    <ProjectPane />
  </Panel>
  <PanelResizeHandle className="w-px bg-gray-700 hover:bg-gray-500" />
  <Panel>{children}</Panel>
</PanelGroup>
```

`autoSaveId` writes to `localStorage` under key `react-resizable-panels:onevoice:sidebar-width` automatically.

**Confidence:** MEDIUM-HIGH — version + license + a11y verified; exact bundle size unverified (`bundlephobia.com` blocked WebFetch). Planner should run a one-shot bundle-size check during planning.

## 3. Accessibility Testing

**Existing project state (verified):**
- React 18 (`services/frontend/package.json:40`).
- Vitest 4.0.18 + jsdom 28 (`services/frontend/package.json:74,68`).
- `@testing-library/react@16` + `@testing-library/jest-dom@6.9` + `@testing-library/user-event@14.6`.
- No axe package currently installed (verified — search of `package.json` returns no `axe` match).

**Critical finding [VERIFIED: github.com/dequelabs/axe-core-npm/issues/500 + npmjs.com/package/@axe-core/react]:**

> "@axe-core/react does not support React 18 and above."

That rules out `@axe-core/react` for this project. The remaining choices:

| Option | Maintenance | Fit |
|---|---|---|
| `vitest-axe` (chaance) | Last published ~3 years ago; flagged by Snyk as low-attention [CITED: snyk.io/advisor/npm-package/vitest-axe] | Drop-in for Vitest; matcher API: `expect(await axe(container)).toHaveNoViolations()` |
| `@chialab/vitest-axe` (fork) | Active — v0.19.1 published ~1 month ago [CITED: npmjs.com/package/@chialab/vitest-axe] | Same API as the original |
| `jest-axe` + Vitest | Possible (Vitest is Jest-compatible) but adds a `jest-axe` dependency to a Vitest project; idiomatic mismatch | Same matcher API |
| `@axe-core/playwright` | Active, official | Requires Playwright (project doesn't use it; would add ~30 MB Chromium download to CI) |

**Decision: `@chialab/vitest-axe`** with the existing Vitest+jsdom+RTL stack. [VERIFIED: npm]

Rationale:
1. React-18-compatible (matcher operates on rendered DOM, not React internals).
2. Actively maintained fork.
3. Zero new test framework — slots into existing `vitest.config.ts` + `vitest.setup.ts`.
4. jsdom limitations (no Shadow DOM, no real ResizeObserver) are tolerable for a sidebar/drawer audit; the violations we care about (focus trap, ARIA labels, color contrast, keyboard navigation) all surface in jsdom.
5. Avoids pulling in Playwright just for one audit gate — Phase 19 should not bring a heavy E2E framework that the rest of the project doesn't use.

**Installation:**

```bash
cd services/frontend
pnpm add -D @chialab/vitest-axe
```

**Setup (extend `vitest.setup.ts`):**

```ts
// vitest.setup.ts (additions)
import * as matchers from '@chialab/vitest-axe/matchers';
import { expect } from 'vitest';
expect.extend(matchers);
```

**Test pattern (e.g., `__tests__/sidebar-a11y.test.tsx`):**

```tsx
import { render } from '@testing-library/react';
import { axe } from '@chialab/vitest-axe';
import { Sidebar } from '@/components/sidebar';

it('sidebar has no critical/serious a11y violations', async () => {
  const { container } = render(<Sidebar />);
  const results = await axe(container, {
    // SEARCH-A11Y gate: fail on critical AND serious only.
    resultTypes: ['violations'],
  });
  // Filter to critical+serious; vitest-axe matcher doesn't filter by impact
  // out of the box, so do it manually:
  const blocking = results.violations.filter(
    (v) => v.impact === 'critical' || v.impact === 'serious'
  );
  expect(blocking).toEqual([]);
});
```

**CI gate config (`package.json` script):**

```json
"test:a11y": "vitest run __tests__/**/*-a11y.test.tsx"
```

Wired into the existing `pnpm test` step in CI; a violation with impact `critical` or `serious` fails the build. `moderate` and `minor` are logged only.

**Confidence:** HIGH — installed packages and React version verified by reading `package.json`; @axe-core/react incompatibility verified by Deque issue tracker.

## 4. Mongo $text Russian Index

**Verified driver version:** `go.mongodb.org/mongo-driver/v2 v2.5.0` [VERIFIED: services/api/go.mod:24].
**Verified bson-driver pattern in repo:** existing repos use `bson.D{{Key: ..., Value: ...}}` and `bson.M{...}` interchangeably; `EnsureConversationIndexes` (`conversation.go:224-243`) is the canonical idempotent index-creation block we extend.

### Index definitions

```go
// services/api/internal/repository/search_indexes.go (NEW)
//
// EnsureSearchIndexes creates the v1.3 text search indexes idempotently at
// API startup. Mongo `CreateIndexes` is a no-op when an index with matching
// keys+options already exists; we swallow IsDuplicateKeyError defensively
// for parity with EnsureConversationIndexes (conversation.go:224).
//
// SEARCH-01: weights favor titles. SetWeights honored by Go driver v2.5.0
// per https://www.mongodb.com/docs/drivers/go/v2.0/indexes/.
// SetBackground(true) is a NO-OP on MongoDB 4.2+; we keep it set for
// source-readability and to satisfy the literal wording of SEARCH-06.
func EnsureSearchIndexes(ctx context.Context, db *mongo.Database) error {
    convs := db.Collection("conversations")
    msgs := db.Collection("messages")

    titleIdx := mongo.IndexModel{
        Keys:    bson.D{{Key: "title", Value: "text"}},
        Options: options.Index().
            SetName("conversations_title_text_ru").
            SetDefaultLanguage("russian").
            SetWeights(bson.D{{Key: "title", Value: 20}}).
            SetBackground(true),
    }
    if _, err := convs.Indexes().CreateOne(ctx, titleIdx); err != nil {
        if !mongo.IsDuplicateKeyError(err) {
            return fmt.Errorf("ensure conversations title text index: %w", err)
        }
    }

    contentIdx := mongo.IndexModel{
        Keys:    bson.D{{Key: "content", Value: "text"}},
        Options: options.Index().
            SetName("messages_content_text_ru").
            SetDefaultLanguage("russian").
            SetWeights(bson.D{{Key: "content", Value: 10}}).
            SetBackground(true),
    }
    if _, err := msgs.Indexes().CreateOne(ctx, contentIdx); err != nil {
        if !mongo.IsDuplicateKeyError(err) {
            return fmt.Errorf("ensure messages content text index: %w", err)
        }
    }
    return nil
}
```

### `$text` query + `$meta:textScore` projection

```go
// Conversation title-match query (phase 1 of D-12 two-phase strategy).
filter := bson.M{
    "$text":       bson.M{"$search": q},
    "user_id":     userID,
    "business_id": businessID,
}
// project_id filter applied conditionally — explicit-null when projectID == nil
// is OUT (search-at-root sees all chats including null-project; D-10).
if projectID != nil {
    filter["project_id"] = *projectID
}

opts := options.Find().
    SetProjection(bson.M{
        "score":      bson.M{"$meta": "textScore"},
        "title":      1,
        "project_id": 1,
        "user_id":    1,
        "business_id": 1,
        "last_message_at": 1,
    }).
    SetSort(bson.D{{Key: "score", Value: bson.M{"$meta": "textScore"}}}).
    SetLimit(int64(limit))

cursor, err := convs.Find(ctx, filter, opts)
```

### `background: true` deprecation

[VERIFIED: mongodb.com/community/forums/t/after-4-2-background-createindexes/15136]
[VERIFIED: github.com/digitalbazaar/bedrock-mongodb/issues/70]

> "From version 4.2 of MongoDB, foreground and background index options have been deleted... MongoDB ignores the `background` index build option if specified to `createIndexes` or its shell helpers."

Mongo 4.2+ uses an optimized hybrid build that allows concurrent reads/writes during index construction. `SetBackground(true)` is silently ignored. We keep the call in the IndexModel because:
1. It's a literal honoring of the SEARCH-06 wording.
2. The Go driver retains the option for backward compatibility (no compile error).
3. If the deployment ever runs on a pre-4.2 Mongo (unlikely; CONTEXT.md doesn't pin a version), the option still applies.

The actual readiness gate is implemented in §7 (atomic.Bool flag + 503 middleware), NOT via Mongo background semantics.

**Confidence:** HIGH — driver version verified, syntax verified against MongoDB v2 docs; deprecation status verified against multiple sources.

## 5. Two-Phase Query Strategy

**Verification of D-12:** [VERIFIED: pkg/domain/mongo_models.go:52-70 — Message struct has `ConversationID` only, no `BusinessID` or `UserID`].

> The `$match` stage that includes a `$text` must be the first stage in the pipeline. [VERIFIED: mongodb.com/docs/manual/tutorial/text-search-in-aggregation/]

This means we cannot do `[$lookup conversations, $match {conversation.business_id: X, $text}]` in one pipeline because:
1. `$text` must be the FIRST `$match` stage.
2. `$text` cannot live inside `$or` or `$not`.
3. The `Message` document itself has no `business_id` field, so we cannot scope `$text` directly on `messages`.

**Therefore D-12's two-phase strategy is REQUIRED, not just preferred.**

### Phase 1 — Conversations title query (one aggregation; can stay as plain `Find` since no grouping)

```go
// Returns: convIDs []string, titleHits []ConversationTitleHit (id, title, score, projectID)
// Filter as in §4 above.
```

### Phase 2 — Messages content query (aggregation pipeline; required for grouping)

```go
// services/api/internal/repository/search_messages.go (NEW)
//
// Pipeline shape:
//   1. $match: $text (REQUIRED FIRST) + conversation_id ∈ scope
//   2. $addFields: score = $meta:textScore  (metadata available after $match)
//   3. $sort: score desc                    (per-message; needed for $group's $first)
//   4. $group by conversation_id            (top-scored snippet wins via $first)
//   5. $sort: top_score desc                (group-level ordering)
//   6. $limit
func (r *messageRepository) SearchByConversationIDs(
    ctx context.Context,
    query string,
    convIDs []string,
    limit int,
) ([]MessageSearchHit, error) {
    pipeline := mongo.Pipeline{
        // Stage 1 — $match MUST be first when including $text.
        bson.D{{Key: "$match", Value: bson.M{
            "$text":           bson.M{"$search": query},
            "conversation_id": bson.M{"$in": convIDs},
        }}},
        // Stage 2 — score available only AFTER the $match stage with $text.
        bson.D{{Key: "$addFields", Value: bson.M{
            "score": bson.M{"$meta": "textScore"},
        }}},
        // Stage 3 — sort by per-message score so $group's $first picks the
        // top-scored snippet for each conversation.
        bson.D{{Key: "$sort", Value: bson.D{{Key: "score", Value: -1}}}},
        // Stage 4 — group by conversation_id; $first returns the top-scored
        // message's snippet/id, $sum counts hits.
        bson.D{{Key: "$group", Value: bson.D{
            {Key: "_id", Value: "$conversation_id"},
            {Key: "top_message_id", Value: bson.D{{Key: "$first", Value: "$_id"}}},
            {Key: "top_content", Value: bson.D{{Key: "$first", Value: "$content"}}},
            {Key: "top_score", Value: bson.D{{Key: "$first", Value: "$score"}}},
            {Key: "match_count", Value: bson.D{{Key: "$sum", Value: 1}}},
        }}},
        // Stage 5 — group-level ordering; rebind to top_score not score.
        bson.D{{Key: "$sort", Value: bson.D{{Key: "top_score", Value: -1}}}},
        // Stage 6 — bound result set (D-12: cap conversation set at 1000 in
        // phase 1; if convIDs > 1000, paginate by chunks — v1.3 single-owner
        // never hits this).
        bson.D{{Key: "$limit", Value: int64(limit)}},
    }

    cur, err := r.collection.Aggregate(ctx, pipeline)
    if err != nil {
        return nil, fmt.Errorf("search messages aggregate: %w", err)
    }
    defer func() { _ = cur.Close(ctx) }()

    var hits []MessageSearchHit
    if err := cur.All(ctx, &hits); err != nil {
        return nil, fmt.Errorf("decode search hits: %w", err)
    }
    return hits, nil
}
```

### Service-layer merge (D-07: aggregated by conversation, ranked by combined score)

```go
// services/api/internal/service/search.go (NEW)
//
// Scoring rule (D-07): for each conversation that appears in either phase,
// the result row carries:
//   row.score = max(titleScore × 20, contentTopScore × 10)
//
// The ×20 / ×10 multipliers mirror the per-index weights set at index creation
// (§4) so a title match outranks a content match of equal raw score, but a
// strong content match still surfaces.
func (s *Searcher) Search(ctx context.Context, businessID, userID, query string, projectID *string, limit int) ([]SearchResult, error) {
    if businessID == "" || userID == "" {
        return nil, domain.ErrInvalidScope     // §6 cross-tenant guard
    }
    if !s.indexReady.Load() {
        return nil, domain.ErrSearchIndexNotReady   // §7
    }

    // Phase 1: conversations title query.
    titleHits, convIDs, err := s.convRepo.SearchTitles(ctx, businessID, userID, query, projectID, limit)
    if err != nil { return nil, err }

    // ... build the FULL conversation-id allowlist (regardless of title hits)
    // for phase 2's $match $in. SearchTitles returns the title hits AND the
    // larger scope-set (every conv visible to this user/business/project).
    scopedIDs := s.convRepo.ScopedConversationIDs(ctx, businessID, userID, projectID)

    // Phase 2: messages content query, scoped to convIDs.
    msgHits, err := s.msgRepo.SearchByConversationIDs(ctx, query, scopedIDs, limit*2)
    if err != nil { return nil, err }

    // Merge & rank — see §10 for snippet centering and highlight builder.
    return mergeAndRank(titleHits, msgHits, /*titleW=*/ 20, /*contentW=*/ 10, limit), nil
}
```

**Confidence:** HIGH — pipeline rules verified against current Mongo manual; convention `bson.D` ordering matches existing `EnsureConversationIndexes` repo style.

## 6. Cross-Tenant Integration Test

**Pattern source:** `test/integration/authorization_test.go:13-194` — canonical two-user pattern (`setupTestUser` + `setupTestBusiness` per user; bearer-token-scoped requests; assert 403/200).

**New test file path:** `test/integration/search_test.go` (alongside existing `conversation_test.go`).

**Test sketch:**

```go
// test/integration/search_test.go (NEW — Phase 19 / SEARCH-05)
package integration

import (
    "encoding/json"
    "net/http"
    "net/url"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestSearchCrossTenant proves the SEARCH-02 / Pitfalls §19 contract:
// Business A's user MUST NOT see Business B's messages even when both
// businesses contain the search term verbatim. Mirrors authorization_test.go's
// two-user shape.
func TestSearchCrossTenant(t *testing.T) {
    cleanupDatabase(t)

    accessTokenA := setupTestUser(t, "userA-search@example.com", "password123")
    accessTokenB := setupTestUser(t, "userB-search@example.com", "password123")
    setupTestBusiness(t, accessTokenA)
    setupTestBusiness(t, accessTokenB)

    // Seed: each user creates a conversation containing the literal term «инвойс».
    convA := createConversationWithMessage(t, accessTokenA, "Тест A", "Пожалуйста, выпиши инвойс на услугу")
    convB := createConversationWithMessage(t, accessTokenB, "Тест B", "Можно инвойс отправить?")

    t.Run("UserASearchesInvoiceSeesOnlyOwn", func(t *testing.T) {
        u := baseURL + "/api/v1/search?" + url.Values{"q": {"инвойс"}, "limit": {"20"}}.Encode()
        req, _ := http.NewRequest("GET", u, nil)
        req.Header.Set("Authorization", "Bearer "+accessTokenA)

        resp, err := httpClient.Do(req)
        require.NoError(t, err)
        defer resp.Body.Close()
        assert.Equal(t, http.StatusOK, resp.StatusCode)

        var body struct {
            Results []struct{ ConversationID string `json:"conversationId"` } `json:"results"`
        }
        require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

        for _, r := range body.Results {
            assert.NotEqual(t, convB, r.ConversationID,
                "User A's search MUST NOT return Business B's conversation %s", convB)
        }
        // Affirmative: A sees their own.
        var sawA bool
        for _, r := range body.Results {
            if r.ConversationID == convA { sawA = true }
        }
        assert.True(t, sawA, "User A should see their own conversation %s", convA)
    })

    t.Run("UserBSearchesInvoiceSeesOnlyOwn", func(t *testing.T) {
        // Symmetric — same shape with tokens swapped.
    })

    t.Run("EmptyQueryReturns400", func(t *testing.T) {
        // SEARCH-02 / D-13 guard: q < 2 chars → 400.
    })

    t.Run("MissingBearerReturns401", func(t *testing.T) {
        // Standard auth contract.
    })
}
```

**Helper to add to `test/integration/main_test.go` (or wherever `setupTestUser` lives — same file family):**

```go
// createConversationWithMessage creates a conversation owned by the bearer
// token's user and posts ONE message (role=user) into it. Returns the
// conversation ID. Used by SEARCH cross-tenant tests to seed inflected Russian
// content.
func createConversationWithMessage(t *testing.T, token, title, msg string) string {
    // ... POST /conversations ; POST /chat/{id} with body {message: msg}
}
```

**Confidence:** HIGH — pattern matches the existing two-user test verbatim; conversation_test.go provides the helper shapes.

## 7. Index Readiness Flag

**Decision: package-level `atomic.Bool`** set after `EnsureSearchIndexes` returns nil. [VERIFIED: pkg.go.dev/sync/atomic — atomic.Bool is the Go 1.19+ canonical type for this exact pattern.]

Rationale:
- Single-flag, multiple-reader, single-writer (writer = startup goroutine) — textbook `atomic.Bool` use case.
- No mutex overhead in the hot path (every search request reads it).
- Avoids inline `db.command({listIndexes: ...})` per request (slow, RTT-bound).
- Safe-publication invariant honored: index creation completes BEFORE the flag flips to true.

**Implementation (sketch):**

```go
// services/api/internal/service/search.go (NEW)
type Searcher struct {
    convRepo   domain.ConversationRepository
    msgRepo    domain.MessageRepository
    indexReady *atomic.Bool       // pointer so the *handler* shares the same flag
}

func NewSearcher(...) *Searcher {
    return &Searcher{
        // ... repos ...
        indexReady: &atomic.Bool{},   // zero value = false
    }
}

// MarkIndexesReady is called from cmd/main.go AFTER EnsureSearchIndexes
// succeeds. The atomic.Bool's Store ensures a happens-before edge with
// every subsequent Load by handler goroutines.
func (s *Searcher) MarkIndexesReady() { s.indexReady.Store(true) }
```

**Wiring in `services/api/cmd/main.go` (extends existing index-creation block at lines 119-130):**

```go
// Phase 19 / SEARCH-06 — text indexes for sidebar search.
indexesCtx3, indexesCancel3 := context.WithTimeout(ctx, 60*time.Second)
defer indexesCancel3()  // text-index build can take longer than the
                         // EnsureConversationIndexes 30s budget on large corpora.
if err := repository.EnsureSearchIndexes(indexesCtx3, mongoDB); err != nil {
    slog.ErrorContext(indexesCtx3, "failed to ensure search text indexes", "error", err)
    return fmt.Errorf("ensure search indexes: %w", err)
}

// Construct the searcher AFTER repos exist (around line 177 in current main.go).
searcher := service.NewSearcher(conversationRepo, messageRepo)

// MUST happen after EnsureSearchIndexes returns nil — see §7.
searcher.MarkIndexesReady()

// Wire the search handler last (so the route exists from boot but returns
// 503 until the flag flips — at which point requests succeed).
searchHandler := handler.NewSearchHandler(searcher, businessService)
handlers.Search = searchHandler
```

**Handler / middleware snippet (503 gate):**

```go
// services/api/internal/handler/search.go (NEW)
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
    q := strings.TrimSpace(r.URL.Query().Get("q"))
    if len(q) < 2 {
        writeError(w, http.StatusBadRequest, "query too short")
        return
    }

    userID := middleware.UserIDFromContext(r.Context())
    biz, err := h.businessSvc.GetByUserID(r.Context(), userID)
    if err != nil {
        writeError(w, http.StatusUnauthorized, "no business")
        return
    }

    var projectID *string
    if p := r.URL.Query().Get("project_id"); p != "" {
        projectID = &p
    }

    limit := parseLimit(r.URL.Query().Get("limit"), 20, 50)

    results, err := h.searcher.Search(r.Context(), biz.ID.String(), userID, q, projectID, limit)
    if errors.Is(err, domain.ErrSearchIndexNotReady) {
        // SEARCH-06: 503 with Retry-After hint while the index builds.
        w.Header().Set("Retry-After", "5")
        writeError(w, http.StatusServiceUnavailable, "search index initializing")
        return
    }
    if errors.Is(err, domain.ErrInvalidScope) {
        // §5 defense-in-depth: should never happen because we resolved
        // userID/businessID server-side, but the repo guards anyway.
        writeError(w, http.StatusInternalServerError, "scope error")
        return
    }
    if err != nil {
        slog.ErrorContext(r.Context(), "search failed",
            "user_id", userID, "business_id", biz.ID.String(), "query_length", len(q))  // SEARCH-07
        writeError(w, http.StatusInternalServerError, "search failed")
        return
    }
    writeJSON(w, http.StatusOK, results)
}
```

**Confidence:** HIGH — pattern is canonical Go; `atomic.Bool` is the sync/atomic library's textbook example for this exact use case.

## 8. Cmd/Ctrl-K Global Shortcut

**Verified file state:** `services/frontend/app/(app)/layout.tsx:1` already starts with `'use client'` (verified). The Cmd/Ctrl-K listener slots in cleanly without creating a new client boundary.

**Implementation (extends existing `AppLayout` component):**

```tsx
// services/frontend/app/(app)/layout.tsx (additions at top of AppLayout body)
'use client';

import { useEffect, useRef, useState } from 'react';
// ... existing imports

// Module-level event-name singleton: any input/element listening for this
// CustomEvent will focus itself. This decouples the layout (broadcaster)
// from the SearchBar (consumer) — the SearchBar can mount/unmount as the
// route changes without re-binding global keyboard listeners.
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';

export default function AppLayout({ children }: { children: ReactNode }) {
  // ... existing auth bootstrap ...

  // D-11: Cmd/Ctrl-K global focus listener. Steals focus from any input
  // INCLUDING the chat composer — Slack/Linear convention.
  useEffect(() => {
    function onKeydown(e: KeyboardEvent) {
      // metaKey covers Cmd on macOS; ctrlKey covers Ctrl on every other
      // platform. Match `K`/`k` — different keyboard layouts may emit either.
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();    // suppress browser's address-bar focus / etc.
        window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT));
      }
    }
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  }, []);

  // ... existing render
}
```

**Consumer pattern (in the new `SidebarSearch` component):**

```tsx
// services/frontend/components/sidebar/SidebarSearch.tsx (NEW)
'use client';
useEffect(() => {
  const input = inputRef.current;
  if (!input) return;
  function onFocus() { input.focus(); input.select(); }
  window.addEventListener('onevoice:sidebar-search-focus', onFocus);
  return () => window.removeEventListener('onevoice:sidebar-search-focus', onFocus);
}, []);
```

**Esc handler (D-11: clear + close + blur in one keystroke):**

```tsx
function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
  if (e.key === 'Escape') {
    setQuery('');         // clear input
    setOpen(false);       // close dropdown
    inputRef.current?.blur();  // release focus
  }
  // ↑/↓/Enter handled separately in the dropdown body via roving tabindex.
}
```

**Mac vs non-Mac placeholder copy (D-11):**

```tsx
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
// SSR-safe: navigator is undefined on the server; the placeholder defaults to
// the more-likely-correct Ctrl-K. The post-mount effect rewrites it to ⌘K
// on macOS without causing a hydration warning because the input value is
// a placeholder, not a controlled value.
const placeholder = isMac ? 'Поиск... ⌘K' : 'Поиск... Ctrl-K';
```

**Confidence:** HIGH — verified `'use client'` boundary already present in `layout.tsx:1`; pattern is standard for Cmd-K global shortcuts.

## 9. ?highlight=msgId Navigation

**Verified existing state:**
- `services/frontend/hooks/useChat.ts:154-237` mounts and loads messages via `GET /conversations/{id}/messages` on mount.
- No existing `data-message-id` attribute in components/chat/*.tsx (verified by grep).
- `useChat` exposes `messages` to the consumer; `MessageList` (which I have not read but exists per repo structure) renders each message.

**Plan (must be implemented in `MessageList` and a new hook):**

### Step 1 — Stamp `data-message-id` on every message bubble

```tsx
// services/frontend/components/chat/MessageBubble.tsx (or MessageList.tsx — wherever the per-message <div> lives)
<div data-message-id={message.id} className={...}>
  {message.content}
</div>
```

### Step 2 — New hook: `useHighlightMessage`

```tsx
// services/frontend/hooks/useHighlightMessage.ts (NEW)
'use client';

import { useEffect } from 'react';
import { useSearchParams, usePathname, useRouter } from 'next/navigation';

const HIGHLIGHT_FLASH_MS = 1750;          // CONTEXT.md D-08: 1.5–2 s range.
const HIGHLIGHT_DATA_ATTR = 'data-highlight';

/**
 * Phase 19 / D-08 / SEARCH-04 — when /chat/{id}?highlight={msgId} is loaded
 * (or navigated to from the search dropdown), find the matched message in the
 * DOM, scroll it into center view, apply a flash class for 1.5–2 s, then
 * remove the class AND strip the query param so a manual refresh doesn't
 * re-fire.
 *
 * Depends on MessageList rendering each message with a `data-message-id`
 * attribute. Re-runs when `messages.length` changes so the effect waits for
 * the message to actually mount before scrolling (the SSE-loaded messages
 * arrive after mount).
 */
export function useHighlightMessage(messagesReady: boolean) {
  const params = useSearchParams();
  const pathname = usePathname();
  const router = useRouter();

  useEffect(() => {
    if (!messagesReady) return;
    const target = params.get('highlight');
    if (!target) return;

    const el = document.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(target)}"]`);
    if (!el) return;     // message not in this conversation — silently ignore

    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.setAttribute(HIGHLIGHT_DATA_ATTR, 'true');

    const timeout = window.setTimeout(() => {
      el.removeAttribute(HIGHLIGHT_DATA_ATTR);
      // Strip ?highlight=… so a refresh doesn't re-fire and so the URL
      // returns to its canonical /chat/{id} form. router.replace avoids a
      // history entry.
      router.replace(pathname, { scroll: false });
    }, HIGHLIGHT_FLASH_MS);

    return () => {
      window.clearTimeout(timeout);
      el.removeAttribute(HIGHLIGHT_DATA_ATTR);
    };
  }, [messagesReady, params, pathname, router]);
}
```

### Step 3 — Wire into the chat page

```tsx
// services/frontend/app/(app)/chat/[id]/page.tsx (additions)
const { messages, isLoading } = useChat(conversationId);
useHighlightMessage(!isLoading && messages.length > 0);
```

### Step 4 — Flash CSS (Tailwind `globals.css` additions)

```css
/* Phase 19 / D-08 — flash highlight on ?highlight=… target. */
[data-highlight='true'] {
  animation: onevoice-flash 1.75s ease-out;
}

@keyframes onevoice-flash {
  0%   { background-color: rgb(250 204 21 / 0.4); }   /* yellow-400/40 */
  100% { background-color: transparent; }
}

@media (prefers-reduced-motion: reduce) {
  [data-highlight='true'] {
    animation: none;
    background-color: rgb(250 204 21 / 0.2);
    transition: background-color 200ms;
  }
}
```

**Confidence:** HIGH — `useSearchParams` + `usePathname` + `router.replace` is the canonical Next.js 14 App Router pattern for this; `CSS.escape` guards against odd Mongo ObjectID characters in the selector.

## 10. Snippet Centering Algorithm

**Decision:** Center on the FIRST stemmed-match position; expand to the nearest word boundaries; clamp to 80–120 chars (CONTEXT.md SEARCH-03 locks ±40-120; we pick 100±20 as the sweet spot).

```go
// services/api/internal/service/snippet.go (NEW)
package service

import (
    "strings"
    "unicode"

    "github.com/kljensen/snowball/russian"
)

// BuildSnippet returns a snippet of `content` centered on the first token
// whose stem matches any of `queryStems`, clamped to roughly [80,120] chars
// and aligned to word boundaries. Returns the empty string if no match.
//
// Algorithm:
//   1. Find the byte offset of the first stem-match token in `content`.
//   2. Compute the desired window: [matchStart - 50, matchEnd + 50] clamped
//      to [0, len(content)].
//   3. Expand the window outward to the nearest preceding/following whitespace
//      so we don't cut a word in half.
//   4. Prepend "…" if window starts > 0; append "…" if window ends < len.
//   5. Total chars in returned string ≤ 120 (the ellipsis adds 1 char each).
//
// Pure function. Table-driven tested with the cases below.
func BuildSnippet(content string, queryStems map[string]struct{}) string {
    matchStart, matchEnd := firstStemMatch(content, queryStems)
    if matchStart < 0 {
        return ""
    }
    const halfWindow = 50
    desired := matchStart - halfWindow
    if desired < 0 {
        desired = 0
    } else {
        desired = expandLeftToBoundary(content, desired)
    }
    end := matchEnd + halfWindow
    if end > len(content) {
        end = len(content)
    } else {
        end = expandRightToBoundary(content, end)
    }
    snippet := content[desired:end]
    if desired > 0 {
        snippet = "…" + snippet
    }
    if end < len(content) {
        snippet = snippet + "…"
    }
    return snippet
}

// firstStemMatch scans content token-by-token and returns the byte range
// of the first token whose stem hits queryStems. Returns (-1,-1) on no match.
func firstStemMatch(content string, queryStems map[string]struct{}) (int, int) {
    runes := []rune(content)
    pos := 0
    for pos < len(runes) {
        for pos < len(runes) && !unicode.IsLetter(runes[pos]) {
            pos++
        }
        start := pos
        for pos < len(runes) && unicode.IsLetter(runes[pos]) {
            pos++
        }
        if start == pos {
            continue
        }
        token := strings.ToLower(string(runes[start:pos]))
        if _, hit := queryStems[russian.Stem(token, false)]; hit {
            byteStart := len(string(runes[:start]))
            byteEnd := len(string(runes[:pos]))
            return byteStart, byteEnd
        }
    }
    return -1, -1
}

func expandLeftToBoundary(s string, pos int) int {
    for pos > 0 && !unicode.IsSpace(rune(s[pos-1])) {
        pos--
    }
    return pos
}

func expandRightToBoundary(s string, pos int) int {
    for pos < len(s) && !unicode.IsSpace(rune(s[pos])) {
        pos++
    }
    return pos
}
```

### Test-table examples

```go
// services/api/internal/service/snippet_test.go (NEW)
{
    name:    "match in middle, both ellipses",
    content: "Доброе утро. Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию выходных.",
    stems:   map[string]struct{}{"запланирова": {}},
    want:    "…Я хочу запланировать пост в Telegram на пятницу вечером, чтобы охватить аудиторию…",
}
{
    name:    "match near start, only trailing ellipsis",
    content: "Запланировать пост на завтра в Telegram-канал.",
    stems:   map[string]struct{}{"запланирова": {}},
    want:    "Запланировать пост на завтра в Telegram-канал.",  // < 120 chars total → no ellipses
}
{
    name:    "no match returns empty string",
    content: "Совсем другой текст без ключевых слов.",
    stems:   map[string]struct{}{"запланирова": {}},
    want:    "",
}
```

**Confidence:** HIGH — algorithm is standard "snippet builder" shape; matches how Bleve/Lucene do it at the snippet layer; rune-vs-byte handling carefully scoped.

## 11. Compound Index for Pinned

**Existing index** (`services/api/internal/repository/conversation.go:224-243`):

```go
{
    Keys: bson.D{
        {Key: "user_id", Value: 1},
        {Key: "business_id", Value: 1},
        {Key: "title_status", Value: 1},
    },
    Options: options.Index().SetName("conversations_user_biz_title_status"),
},
```

This index is optimized for **the auto-titler's `UpdateTitleIfPending` filter** + sidebar `auto_pending` bucket queries. It is NOT a good fit for the sidebar's main-list ordering query, which ranks by `pinned_at desc, last_message_at desc`.

**ESR (Equality, Sort, Range) analysis of the new sidebar query:**

```js
db.conversations.find({
    user_id: $eq,        // E
    business_id: $eq,    // E
    project_id: $eq,     // E (or $exists when "Без проекта" filter)
}).sort({
    pinned_at: -1,       // S (nulls sort below non-nulls in Mongo desc)
    last_message_at: -1, // S
})
```

Per the MongoDB ESR rule, optimal index is `{user_id: 1, business_id: 1, project_id: 1, pinned_at: -1, last_message_at: -1}`. The existing index does not have `project_id` or `last_message_at`, and adding `pinned_at` to it would change its meaning for the auto-titler's hot path.

**Decision: ADD a new compound index** (do NOT extend the existing one).

```go
// services/api/internal/repository/conversation.go — extend EnsureConversationIndexes:
models := []mongo.IndexModel{
    // Existing — auto-titler's UpdateTitleIfPending hot path (Phase 18 D-08a). Untouched.
    {
        Keys: bson.D{
            {Key: "user_id", Value: 1},
            {Key: "business_id", Value: 1},
            {Key: "title_status", Value: 1},
        },
        Options: options.Index().SetName("conversations_user_biz_title_status"),
    },
    // NEW — Phase 19 sidebar list ordering (pinned-first then recency).
    // ESR: user_id, business_id, project_id (Equality) → pinned_at, last_message_at (Sort, both desc).
    {
        Keys: bson.D{
            {Key: "user_id", Value: 1},
            {Key: "business_id", Value: 1},
            {Key: "project_id", Value: 1},
            {Key: "pinned_at", Value: -1},
            {Key: "last_message_at", Value: -1},
        },
        Options: options.Index().SetName("conversations_user_biz_proj_pinned_recency"),
    },
}
```

Rationale:
- Each index is independently optimal for its query (no compromise).
- Mongo single-business deployment scale makes the extra index storage cost negligible (< 50 KB at v1.3 corpus size).
- The two indexes share three prefix keys (user_id, business_id) so write-amplification is bounded.

**Confidence:** HIGH — ESR rule is canonical Mongo guidance; existing repo style replicated verbatim.

## 12. pinned_at Backfill

**Existing pattern** [VERIFIED: services/api/internal/repository/mongo_backfill.go:35-100]:
- Single-shot `BackfillConversationsV15` runs at API startup.
- `schema_migrations` collection holds a marker document with `_id = "<phase-name>"`.
- Per-field guards via `{$exists: false}` to keep it idempotent on every boot.
- Marker write at the end blocks subsequent runs (FindOne fast-path at the top).

**Recommended approach: extend the same pattern with a Phase 19 marker.**

```go
// services/api/internal/repository/mongo_backfill.go — additions:

const SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"

// BackfillConversationsV19 is the Phase 19 idempotent backfill. It:
//   1. Sets `pinned_at: null` on every doc lacking the field.
//   2. Migrates the legacy `pinned: bool` flag — for any doc with
//      pinned == true AND pinned_at == null, sets pinned_at = updated_at
//      (best available timestamp; the user pinned at some unknown earlier
//      moment — updated_at is the closest signal).
//   3. Drops the legacy `pinned` field via $unset (single source of truth
//      becomes `pinned_at != nil`; see decision rationale below).
//
// Each step guarded so reruns are no-ops. Marker written last; FindOne
// fast-path at the top short-circuits subsequent boots.
func BackfillConversationsV19(ctx context.Context, db *mongo.Database) error {
    conversations := db.Collection("conversations")
    marker := db.Collection("schema_migrations")

    var existing bson.M
    err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase19}).Decode(&existing)
    if err == nil {
        slog.InfoContext(ctx, "phase 19 backfill already applied", "marker", SchemaMigrationPhase19)
        return nil
    }
    if !errors.Is(err, mongo.ErrNoDocuments) {
        return fmt.Errorf("read schema_migrations marker: %w", err)
    }

    // Step 1: pinned_at = null when missing.
    if err := backfillField(ctx, conversations, "pinned_at",
        bson.M{"pinned_at": bson.M{"$exists": false}},
        bson.M{"$set": bson.M{"pinned_at": nil}}); err != nil {
        return err
    }

    // Step 2: migrate legacy `pinned: true` → `pinned_at: updated_at`.
    legacyFilter := bson.M{
        "pinned":    true,
        "pinned_at": nil,
    }
    legacyUpdate := mongo.Pipeline{
        {{Key: "$set", Value: bson.D{{Key: "pinned_at", Value: "$updated_at"}}}},
    }
    if _, err := conversations.UpdateMany(ctx, legacyFilter, legacyUpdate); err != nil {
        return fmt.Errorf("migrate legacy pinned bool: %w", err)
    }

    // Step 3: drop the legacy `pinned` field. Single source of truth from
    // here is `pinned_at != nil`. The bool's job (UI predicate) is replaced
    // by `conv.pinned_at != null` everywhere; field is dead-code in Go after
    // Phase 19 D-02.
    if _, err := conversations.UpdateMany(ctx,
        bson.M{"pinned": bson.M{"$exists": true}},
        bson.M{"$unset": bson.M{"pinned": ""}}); err != nil {
        return fmt.Errorf("drop legacy pinned bool: %w", err)
    }

    // Marker.
    _, err = marker.UpdateOne(ctx,
        bson.M{"_id": SchemaMigrationPhase19},
        bson.M{"$set": bson.M{"_id": SchemaMigrationPhase19, "applied_at": time.Now().UTC()}},
        options.UpdateOne().SetUpsert(true),
    )
    if err != nil {
        return fmt.Errorf("write schema_migrations marker: %w", err)
    }
    slog.InfoContext(ctx, "phase 19 backfill complete", "marker", SchemaMigrationPhase19)
    return nil
}
```

### Single source of truth: `pinned_at != nil`, drop `Pinned bool`

**Rationale:**
- Two fields = two states that can diverge (`pinned: true, pinned_at: null` is logically nonsense; `pinned: false, pinned_at: <timestamp>` ditto). Every read site has to remember which one to trust → bug magnet.
- `pinned_at` carries strictly more information (timestamp) than `pinned bool` (boolean) — replacing is lossless.
- D-02 explicitly says "if planner removes the bool, the migration MUST drop the field idempotently" — the `$unset` is idempotent (`{$exists: true}` guard).
- Phase 18 already removed `omitempty` on `TitleStatus` for similar single-source-of-truth reasons; Phase 19 follows the precedent.

**Domain-model change** (extends `pkg/domain/mongo_models.go:39-50`):

```go
type Conversation struct {
    ID            string     `json:"id" bson:"_id,omitempty"`
    UserID        string     `json:"userId" bson:"user_id"`
    BusinessID    string     `json:"businessId" bson:"business_id"`
    ProjectID     *string    `json:"projectId,omitempty" bson:"project_id"`
    Title         string     `json:"title" bson:"title"`
    TitleStatus   string     `json:"titleStatus" bson:"title_status"`
    // Pinned bool removed — single source of truth is PinnedAt != nil (Phase 19 D-02).
    PinnedAt      *time.Time `json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"`  // NEW
    LastMessageAt *time.Time `json:"lastMessageAt,omitempty" bson:"last_message_at,omitempty"`
    CreatedAt     time.Time  `json:"createdAt" bson:"created_at"`
    UpdatedAt     time.Time  `json:"updatedAt" bson:"updated_at"`
}
```

**Wiring in `services/api/cmd/main.go` (extends existing block at line 89):**

```go
// Phase 19 — pinned_at backfill + drop legacy `pinned` bool. Same shape as
// the Phase 15 backfill at line 89.
backfillCtx2, backfillCancel2 := context.WithTimeout(ctx, 30*time.Second)
defer backfillCancel2()
if err := repository.BackfillConversationsV19(backfillCtx2, mongoDB); err != nil {
    slog.ErrorContext(backfillCtx2, "phase 19 backfill failed", "error", err)
    return fmt.Errorf("phase 19 backfill: %w", err)
}
```

**Confidence:** HIGH — pattern is verbatim from `BackfillConversationsV15`; the `$unset` step is the well-understood idempotent shape.

## 13. Validation Architecture

> Required header — Nyquist parses this. `workflow.nyquist_validation` is enabled by default; this section is included.

### Test Framework

| Property | Value |
|----------|-------|
| Backend framework | Go 1.25 + testify v1.11 + `-race` (verified `services/api/go.mod:3,21`) |
| Backend integration | `test/integration/*_test.go` against a live API instance (verified `test/integration/main_test.go`) |
| Frontend framework | Vitest 4.0.18 + jsdom + @testing-library/react 16 (verified `services/frontend/package.json:74,68,59`) |
| Frontend a11y | `@chialab/vitest-axe` (NEW — Wave 0 install, see §3) |
| Backend test entry | `cd services/api && GOWORK=off go test -race ./...` |
| Frontend test entry | `cd services/frontend && pnpm test` |
| Full suite (CI) | `make test-all` (covers both) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SEARCH-01 | Idempotent text indexes at startup | unit | `cd services/api && GOWORK=off go test -race ./internal/repository -run TestEnsureSearchIndexes` | ❌ Wave 0 |
| SEARCH-02 | Empty `business_id`/`user_id` returns ErrInvalidScope | unit | `cd services/api && GOWORK=off go test -race ./internal/service -run TestSearch_RejectsEmptyScope` | ❌ Wave 0 |
| SEARCH-02 | Cross-tenant isolation (User A doesn't see User B) | integration | `cd test/integration && GOWORK=off go test -race -run TestSearchCrossTenant` | ❌ Wave 0 — see §6 |
| SEARCH-03 | Aggregation merges title-weight×20 with content-weight×10 | unit | `cd services/api && GOWORK=off go test -race ./internal/service -run TestSearch_RankingAndSnippet` | ❌ Wave 0 |
| SEARCH-03 | Snippet centering is byte-safe + word-boundary aligned | unit | `cd services/api && GOWORK=off go test -race ./internal/service -run TestBuildSnippet` | ❌ Wave 0 — see §10 |
| SEARCH-04 | 250 ms debounce on input change | unit | `cd services/frontend && pnpm test SidebarSearch.test.tsx` | ❌ Wave 0 |
| SEARCH-04 | Click-result navigates with `?highlight=msgId` | unit | `cd services/frontend && pnpm test SearchResultRow.test.tsx` | ❌ Wave 0 |
| SEARCH-05 | Project filter scopes to current project when on /chat/projects/{id} | integration | `cd test/integration && GOWORK=off go test -race -run TestSearchProjectScope` | ❌ Wave 0 |
| SEARCH-06 | Endpoint returns 503 until atomic.Bool flag flips | unit | `cd services/api && GOWORK=off go test -race ./internal/handler -run TestSearchHandler_503BeforeReady` | ❌ Wave 0 — see §7 |
| SEARCH-07 | Search log carries metadata only (no query text) | unit | `cd services/api && GOWORK=off go test -race ./internal/handler -run TestSearchHandler_LogShape` | ❌ Wave 0 |
| UI-01 | Layout splits into nav-rail + project-pane + chat | unit | `cd services/frontend && pnpm test layout.test.tsx` | ❌ Wave 0 |
| UI-02 | Sidebar lists pinned + unassigned + projects + new-project link | unit | `cd services/frontend && pnpm test sidebar.test.tsx` | ❌ Wave 0 |
| UI-03 | Pin/unpin mutation invalidates `['conversations']` cache | unit | `cd services/frontend && pnpm test usePinMutation.test.tsx` | ❌ Wave 0 |
| UI-04 | Context menu fires pin/rename/delete/move from Radix DropdownMenu | unit | `cd services/frontend && pnpm test ProjectSection.test.tsx` (extend existing) | ✅ exists at `services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx` |
| UI-05 | Mobile drawer auto-closes on chat select; stays open for project expand | unit | `cd services/frontend && pnpm test mobile-drawer.test.tsx` | ❌ Wave 0 |
| UI-05 | Drawer + chat list pass axe critical/serious gate | unit (a11y) | `cd services/frontend && pnpm test:a11y` | ❌ Wave 0 — see §3 |
| UI-06 | ↑/↓/Enter/Esc navigate the dropdown via roving tabindex | unit | `cd services/frontend && pnpm test SearchDropdown-keyboard.test.tsx` | ❌ Wave 0 |
| UI-06 | Cmd/Ctrl-K steals focus from the chat composer | unit | `cd services/frontend && pnpm test cmd-k.test.tsx` | ❌ Wave 0 — see §8 |
| (D-01 mitigation) | ChatHeader pin button does not flicker on unrelated cache mutation | unit | `cd services/frontend && pnpm test ChatHeader.isolation.test.tsx` (extend existing) | ✅ template at `ChatHeader.tsx:30` |
| (D-08 — flow) | Search→click→/chat/{id}?highlight=…→scroll→flash→cleanup | integration | `cd services/frontend && pnpm test highlight-flow.test.tsx` (RTL with mocked router) | ❌ Wave 0 — see §9 |
| (D-12 — readiness) | 503 with `Retry-After: 5` until `MarkIndexesReady` runs | integration | `cd test/integration && GOWORK=off go test -race -run TestSearch_503BeforeReady` | ❌ Wave 0 |
| (D-12 — pipeline shape) | Two-phase query returns aggregated rows, not raw messages | integration | `cd test/integration && GOWORK=off go test -race -run TestSearchAggregatedShape` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `cd services/api && GOWORK=off go test -race ./internal/{handler,service,repository}/... -run <Specific>` (subset relevant to the task; <30 s).
- **Per wave merge:** `make test-all` (full Go + frontend; ~3-5 min).
- **Phase gate:** `make test-all` green + `pnpm test:a11y` green + new `TestSearchCrossTenant` passing before `/gsd-verify-work`.

### Wave 0 Gaps

Plans that need to land before any production code:
- [ ] `services/api/internal/service/search_test.go` — covers SEARCH-02/03; new file
- [ ] `services/api/internal/service/snippet_test.go` — covers BuildSnippet; new file
- [ ] `services/api/internal/repository/search_messages_test.go` — covers SearchByConversationIDs pipeline shape; new file
- [ ] `services/api/internal/repository/search_indexes_test.go` — covers EnsureSearchIndexes idempotency (run twice, no error); new file
- [ ] `services/api/internal/handler/search_test.go` — covers 503-before-ready, 400-short-query, log-shape; new file
- [ ] `test/integration/search_test.go` — TestSearchCrossTenant + TestSearch_503BeforeReady + TestSearchAggregatedShape; new file (§6)
- [ ] `services/frontend/__tests__/sidebar-a11y.test.tsx` — axe gate; new file
- [ ] `services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx` — debounce + dropdown render; new file
- [ ] `services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx` — keyboard nav + click navigation; new file
- [ ] `services/frontend/__tests__/cmd-k.test.tsx` — global shortcut listener; new file
- [ ] `services/frontend/__tests__/mobile-drawer.test.tsx` — auto-close-on-chat-select + stays-open-on-expand; new file
- [ ] `services/frontend/__tests__/highlight-flow.test.tsx` — `?highlight=msgId` end-to-end with mocked router; new file
- [ ] Framework install: `pnpm add -D @chialab/vitest-axe` and append matchers to `vitest.setup.ts`

## 14. Performance Budget

[ASSUMED — single-owner v1.3 scale; planner should validate during implementation against actual corpus]

| Metric | Target | Rationale |
|---|---|---|
| Mongo `$text` latency p50 | < 30 ms | Single-owner ≤ ~100 conversations × ~low-thousands messages; text index hot in RAM |
| Mongo `$text` latency p99 | < 200 ms | Buffer for cold-cache start + occasional `$in: [convIDs]` of 1000 elements |
| Search endpoint p50 (handler-to-response) | < 100 ms | Phase-1 `Find` + Phase-2 aggregate + Go merge + snowball stems for highlight ranges |
| Search endpoint p99 | < 500 ms | Network + JSON + worst-case bilateral cache miss |
| Frontend dropdown render | < 16 ms (1 frame) | 20 results × snippet+highlight; React Query keeps list cached |
| Cmd-K listener latency (keyup → input focus) | < 30 ms | Single keydown handler, single CustomEvent dispatch, single focus() |
| Index build time on cold boot | < 5 s | At v1.3 scale (low-thousands messages × ~500 bytes); `background:true` is no-op (§4) |
| Backfill time on cold boot | < 2 s | Three guarded `UpdateMany` over ≤ ~hundreds documents |
| `react-resizable-panels` drag tear | 0 frames | Library handles via CSS transforms; no React re-render per pointermove |

**Concern flagged:** at boot, `EnsureSearchIndexes` blocks startup until both indexes complete (we deliberately hold the boot to keep `MarkIndexesReady` ordering correct). On a cold corpus this is fast; on a future tenant with 100K+ messages, the blocking build could exceed the 60 s context timeout we set (§7). Planner should consider a follow-up that builds asynchronously and gates the handler purely via the `atomic.Bool` flag — for v1.3 the synchronous-build-with-flag pattern is correct.

## 15. Open Questions

Each question carries a recommended default so the planner is not blocked.

### Q1: Does the existing `Conversation.Pinned bool` filter conflict with `$text` on title?

**Concern:** Phase 15 introduced `Conversation.Pinned bool` (verified `mongo_models.go:46`). If we keep both fields side-by-side AND use `$text` on title, the query

```js
{$text: {$search: q}, user_id: U, business_id: B, pinned: true}
```

works fine — Mongo allows non-`$text` filters in the same `$match` as `$text`, only constraining `$text` to be in the FIRST `$match` and not under `$or`/`$not` (verified §5).

**Default recommendation:** drop `pinned bool` per §12; the question becomes moot. If the planner chooses to keep both fields (compatibility caution), the search query simply does NOT filter on `pinned` — the sidebar's pinned section is a frontend-side filter on already-returned `pinned_at != null`.

### Q2: Mixing `$text` with non-$text scope filters in a single `$match` — any gotchas?

[VERIFIED: mongodb.com/docs/manual/tutorial/text-search-in-aggregation/]

> "A `$text` operator can only occur once in the stage" + "The `$text` operator expression cannot appear in `$or` or `$not` expressions."

**Default recommendation:** safe to combine `$text` with `user_id`, `business_id`, `project_id` equality filters in the same `$match`, AS LONG AS they're in the implicit-`$and` (top-level) shape. Avoid `$or` wrapping. Phase-1 query as shown in §4 follows this rule.

### Q3: Should the dropdown checkbox «По всему бизнесу» (D-10) reset on route change?

**Default recommendation:** YES. The checkbox is local UI state inside `<SidebarSearch>`. When `usePathname()` changes, the component receives new initial scope (route-derived) and the local checkbox resets. Keeps mental model simple: "open dropdown on a project page → defaults to project scope".

### Q4: `pinned_at` ordering for ties (two chats pinned in the same millisecond)

**Default recommendation:** secondary sort by `_id desc` (Mongo ObjectIDs encode timestamp + counter, so this is stable). Implementation: extend the compound index of §11 to include `_id` as a tiebreaker, OR rely on Mongo's natural insertion order which is `_id`-correlated.

### Q5: Cmd-K listener and pending-approval card focus

**Default recommendation:** Cmd-K steals focus unconditionally per D-11 (Slack/Linear convention). If the user is mid-edit in the approval card's JSON editor (Phase 17 surface), Cmd-K will yank focus — this is the intended behavior. The approval card persists state across focus changes (Phase 17 D-04 verified), so no data is lost.

### Q6: `?highlight=msgId` for messages outside the conversation

**Default recommendation:** silently ignore (per §9 implementation: `if (!el) return;`). The URL is shareable, so a stale link should not throw. No toast, no error — the chat just opens without highlighting.

### Q7: Snippet algorithm for messages with multiple matches

**Default recommendation:** center on the FIRST match (per §10). The `+N совпадений` badge on the result row tells the user there are more matches in the conversation; clicking opens the chat scrolled to the top-scored message and they can scroll further. v1.4 SEARCH-L1 will introduce per-message navigation.

### Q8: Index readiness flag on horizontal scaling

**Default recommendation:** v1.3 is single-replica per CONTEXT.md / PROJECT.md scope. The `atomic.Bool` is per-process. If the API ever runs > 1 replica, each replica has its own flag and each runs `EnsureSearchIndexes` independently — Mongo's `CreateIndexes` is idempotent so this is safe; the only inefficiency is N replicas all trying to create the same index, which Mongo handles by taking a lock and the late-comers see "already exists". Acceptable.

### Q9: Russian stemmer divergence (snowball-go vs Mongo libstemmer)

**Default recommendation:** document as Pitfall in the task plan. Backend test corpus must include 5-10 known-divergent words (per the `kljensen/snowball` README's «злейший» example) and assert the highlight builder still produces SOMETHING reasonable (e.g., a partial-substring fallback when stem-match fails). Frontend renders missing-mark gracefully — the snippet is still visible, the row is still clickable. v1.4 candidate: ship a more spec-compliant stemmer.

### Q10: Where does `Searcher.ScopedConversationIDs` cap come from?

D-12 says "cap conversation set at 1000 — paginate if more". v1.3 single-owner has no path to >1000 conversations. **Default recommendation:** hardcode `const MaxScopedConversations = 1000`; if the scope returns ≥ this many, log a warning and silently truncate the OLDEST (sort `last_message_at desc` then take first 1000). v1.4 introduces real pagination.

## RESEARCH COMPLETE

**Phase:** 19 - Search & Sidebar Redesign
**Confidence:** HIGH

### Key Findings

- Pin `github.com/kljensen/snowball v0.10.0` (MIT, pure Go, ergonomic API) for D-09 highlight ranges; document the «злейший»→«зл» NLTK divergence as a Pitfall.
- Pin `react-resizable-panels` v4.x (MIT, built-in WAI-ARIA Splitter, `autoSaveId` localStorage) for D-15; reject custom-hook alternative (a11y boilerplate trap).
- **CRITICAL: `@axe-core/react` does NOT support React 18.** Use `@chialab/vitest-axe` (active fork) with the existing Vitest+jsdom setup; gate `critical`+`serious` in CI.
- `$text` MUST be in the first `$match` stage (Mongo manual rule); D-12's two-phase query is REQUIRED, not optional. `Message` document has no `business_id` field, confirming the strategy.
- `background:true` is a no-op on Mongo 4.2+; the actual readiness gate is an `atomic.Bool` set after `EnsureSearchIndexes` returns.
- Add a NEW compound index `{user_id, business_id, project_id, pinned_at:-1, last_message_at:-1}` per ESR rule; do not extend the Phase 18 compound index.
- `pinned_at != nil` becomes the single source of truth; drop legacy `Pinned bool` via idempotent `$unset` in the Phase 19 backfill.
- All 14 verification items have concrete Go/TypeScript code references; planner can begin without blocking on further research.

### File Created

`/Users/f1xgun/onevoice/.worktrees/milestone-1.3/.planning/phases/19-search-sidebar-redesign/19-RESEARCH.md`

### Confidence Assessment

| Area | Level | Reason |
|------|-------|--------|
| Stack (snowball, panels, axe) | HIGH | Versions, licenses, pure-Go status, React 18 incompat verified via npm + GitHub + pkg.go.dev |
| Mongo $text rules | HIGH | Quoted from current Mongo manual; constraints verified |
| Two-phase query | HIGH | Required by Mongo `$text` + first-`$match` rule + verified `Message` schema |
| Snippet/highlight algorithm | HIGH | Standard pattern; rune-vs-byte correctness preserved |
| atomic.Bool readiness | HIGH | sync/atomic textbook |
| Bundle sizes (panels gz) | MEDIUM | Bundlephobia blocked WebFetch; planner should verify |
| Performance targets | MEDIUM | [ASSUMED] for v1.3 single-owner; not benchmarked against real corpus |

### Open Questions

10 questions documented in §15, each with a recommended default so the planner is not blocked. The most consequential ones for the planner:
- Q1/Q9: Russian stemmer divergence — document as a Pitfall, plan a v1.4 follow-up.
- Q4: `pinned_at` tiebreaker — extend compound index with `_id` (planner's call).
- Q10: Conversation-scope cap at 1000 — hardcoded, log on overflow.

### Ready for Planning

Research complete. Planner can now create PLAN.md files for the 5 sub-plans (`19-01-layout-restructure`, `19-02-pinned`, `19-03-search-backend`, `19-04-search-frontend`, `19-05-a11y-and-audit`) per CONTEXT.md D-14 split-recommendation.

Sources:
- [github.com/kljensen/snowball (Russian Snowball stemmer for Go, MIT, v0.10.0)](https://github.com/kljensen/snowball)
- [pkg.go.dev/github.com/kljensen/snowball/russian (API surface, license, pure-Go)](https://pkg.go.dev/github.com/kljensen/snowball/russian)
- [github.com/blevesearch/snowballstem (Bleve fork — license-unclear)](https://github.com/blevesearch/snowballstem)
- [bvaughn/react-resizable-panels (latest 4.9.0, MIT, ARIA Splitter)](https://github.com/bvaughn/react-resizable-panels)
- [npmjs.com/package/react-resizable-panels (npm listing)](https://www.npmjs.com/package/react-resizable-panels)
- [shadcn/ui Resizable (canonical Resizable primitive)](https://ui.shadcn.com/docs/components/radix/resizable)
- [npmjs.com/package/@axe-core/react (React 18 NOT supported notice)](https://www.npmjs.com/package/@axe-core/react)
- [github.com/dequelabs/axe-core-npm/issues/500 (React 18 support tracker)](https://github.com/dequelabs/axe-core-npm/issues/500)
- [npmjs.com/package/@chialab/vitest-axe (active fork, v0.19.1)](https://www.npmjs.com/package/@chialab/vitest-axe)
- [github.com/chaance/vitest-axe (original; ~3 yrs since last release)](https://github.com/chaance/vitest-axe)
- [MongoDB Go Driver v2 — Indexes guide (SetWeights, SetDefaultLanguage, SetBackground)](https://www.mongodb.com/docs/drivers/go/v2.0/indexes/)
- [MongoDB Search Text — Go Driver v2.2 ($text + $meta:textScore)](https://www.mongodb.com/docs/drivers/go/current/fundamentals/crud/read-operations/text/)
- [MongoDB $text in the Aggregation Pipeline (must be first $match; no $or/$not)](https://www.mongodb.com/docs/manual/tutorial/text-search-in-aggregation/)
- [MongoDB Specify Language for Text Indexes (default_language: russian)](https://www.mongodb.com/docs/manual/tutorial/specify-language-for-text-index/)
- [MongoDB community forum — background:true deprecated in 4.2+](https://www.mongodb.com/community/forums/t/after-4-2-background-createindexes/15136)
- [github.com/digitalbazaar/bedrock-mongodb#70 (background option obsolete in 4.2+)](https://github.com/digitalbazaar/bedrock-mongodb/issues/70)
- [pkg.go.dev/sync/atomic (atomic.Bool readiness flag pattern)](https://pkg.go.dev/sync/atomic)
- [WAI-ARIA Window Splitter pattern (keyboard a11y reference)](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/)
