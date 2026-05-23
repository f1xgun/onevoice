# ADR-0002 — Do not split `services/api/internal/service/` into pure-domain + persistence halves

- **Status:** Accepted
- **Date:** 2026-05-23
- **Scope:** `services/api/internal/service/`

## Context

`services/api/internal/service/` holds ~9200 LOC across 25 files
(business, integration, user, project, review, search, hitl, oauth,
titler, snippet, etc.). Each service follows the same shape:

```go
func (s *xxxService) Method(ctx, ...) (..., error) {
    // 1. validation (5–15 LOC, inline at method start)
    // 2. repo write/read
    // 3. audit.LogXxx(...)        — AFTER successful commit
    return result, nil
}
```

An architectural review (`/improve-codebase-architecture`, v2 —
2026-05-23) flagged this layer as a **Speculative** "Persistence vs
domain side effects split" deepening opportunity. The reviewer's
candidate refactor proposed one of:

- **A.** Extract pure-domain `Validate(b)` functions per type.
- **B.** Wrap each service in an `AuditedXxxService` decorator that
  emits audit rows around an inner core.
- **C.** Apply hexagonal / onion architecture — split each service into
  a pure-domain core and a persistence adapter.
- **D.** Move "domain projections" (`b.ToolApprovals()`, etc.) onto
  richer domain types.

A spot-check across `business.go` (348 LOC), `integration.go` (434 LOC),
and representative slices of the rest of the layer showed:

1. **Handler → Service → Repository discipline is already enforced.**
   `services/api/AGENTS.md` documents the rule under "Layer Rules";
   no service method skips a layer.
2. **Audit emission timing is already correct.** Every
   `audit.Log...(...)` call sits AFTER `tx.Commit()` or after a
   successful single-statement repo write. There is no scenario where a
   side effect fires for an unwritten row.
3. **Side effects are already abstracted.** Audit logging flows through
   a single `audit.Logger` interface with a `audit.Nop()` fallback for
   tests — there is no scatter of ad-hoc logging code.
4. **Domain richness already lives on the right types.** Methods like
   `*domain.Business.ToolApprovals()` exist on the domain struct; the
   service layer delegates rather than re-implementing.
5. **Validation is short, inline, and single-callsite.** Each service
   method's validation block is 5–15 LOC and has exactly one caller
   (the handler).

## Decision

**Keep `services/api/internal/service/` as-is.** Do not introduce a
pure-domain / persistence-adapter split.

The reasoning is rooted in the architecture-review methodology this
repo follows:

1. **Deletion test on the reviewer's candidates:**
   - **A (extract Validate):** Removing `domain.ValidateBusiness(b)`
     and inlining the 10 LOC back into `Create()` produces an
     identical-looking method. The "abstraction" carries no behavior
     beyond the call. Fails.
   - **B (AuditedService decorator):** Removing the decorator and
     putting `audit.LogXxx(...)` calls back inline after each commit
     produces the code we have today. Fails: today's code IS already
     the post-deletion state.
   - **C (hexagonal split):** Removing the I/O adapter and inlining
     would re-produce today's 10–40-LOC methods. The "pure domain"
     core would be 5 LOC of validation in most cases; this is not
     depth, it is ceremony.

2. **Two-adapters rule:** Each service method has exactly **one**
   consumer (the matching HTTP handler). The "validation" surface and
   the "domain core" surface have zero callers outside the service
   itself. Neither earns a seam.

3. **Premature consolidation risk:** Each of A/B/C trades the present
   linear "validate → write → audit" shape for a multi-level dispatch
   (validator + service + audit decorator + repo). For services of
   ~10–40 LOC per method this **adds reading cost** without
   concentrating any new behavior.

## Alternatives considered

### A. Pure-domain `Validate()` functions

```go
// pkg/domain/business_validation.go
func ValidateBusinessCreate(b *Business, ownerUserID uuid.UUID) error { ... }
```

Rejected — validation is 5–15 LOC per method, single-callsite, no
sharing requirement. The extracted function adds a layer without
earning depth. If a future feature needed shared validation across
services (e.g. multiple admin/operator paths writing the same domain
object) this would graduate from premature to justified — see
**Reconsider when** below.

### B. `AuditedXxxService` decorator

```go
type AuditedBusinessService struct {
    inner BusinessService
    audit audit.Logger
}
func (a *AuditedBusinessService) Create(ctx, b, owner) (*Business, error) {
    out, err := a.inner.Create(ctx, b, owner)
    if err != nil { return nil, err }
    audit.LogBusinessCreated(ctx, a.audit, b.ID, owner, b.Name)
    return out, nil
}
```

Rejected — every service method gains a thin delegation. Audit lines
already sit at the right call-site (AFTER commit). The decorator
relocates them from method body to wrapper class without changing the
production trace. Maintenance burden grows N × (services × methods)
without earning a callable seam.

### C. Hexagonal / onion split

Rejected — moving I/O out of services leaves cores of ~5 LOC. The
"hexagonal" pattern earns its keep when business rules are complex
enough to test independently of I/O. Here the business rules per
method ARE the validation and the audit-after-commit ordering, which
are best read in linear flow. Textbook clean, codebase reality slop.

### D. Domain richness

Already in place — `b.ToolApprovals()`,
`businessMembership.IsActive()`, `project.WhitelistMode`, etc. are all
methods on the domain types. Service methods correctly delegate
rather than re-implementing projection logic.

### E. SKIP without ADR

Rejected — without this record, the same Speculative candidate
reappears on every future architectural sweep. ADRs exist precisely to
record load-bearing skip decisions so reviewers (human and AI) can see
the prior reasoning instead of re-deriving it from scratch.

## Consequences

**Service-layer reviewers should keep the linear shape.** When adding a
new service method, follow the established pattern:

1. Validate inputs inline at the method start (no `valid` package).
2. Open a transaction only if multiple writes need atomicity.
3. Perform repo writes.
4. After successful commit, emit `audit.LogXxx(ctx, s.audit, ...)`.

**Cross-cutting concerns that DO warrant extraction:** transaction
management (already inline via `pool.BeginTx`), encryption (already in
`crypto.Encryptor`), token refresh (already in `TokenRefresher`).
These earn their seams because they have multiple callers OR encode
non-trivial behavior that would be unsafe to scatter.

## Reconsider when

This ADR should be revisited and likely superseded by an ADR-0003 if
any of the following lands:

- **Two or more services need to share validation** for the same
  domain type (e.g. an `admin/` path and a `public/` path both write
  to `domain.Business` and must enforce the same invariants). At that
  point, `pkg/domain/{type}_validation.go` becomes a real seam.
- **Audit emission acquires non-trivial logic** (e.g. async dispatch
  to a separate audit log store with retry, batching, or PII
  redaction). At that point, a `AuditedXxxService` decorator OR
  middleware-style observer pattern earns its seam.
- **A service grows past ~500 LOC** with mixed concerns (e.g. business
  rules + cache invalidation + dispatching). At that point, the
  hexagonal split has a real friction to solve, not a hypothetical one.

At any of those triggers, this ADR should be revisited and superseded
with the new context.

## Related

- [ADR-0001](0001-prompt-locale-pair-rendering.md) — same methodology
  applied to the orchestrator's prompt builder (also SKIP + ADR).
