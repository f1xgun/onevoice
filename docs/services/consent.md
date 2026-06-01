# Consent service

`services/api/internal/service/consent.go` (`ConsentService`) orchestrates the three legal-consent flows that Register, the ReConsent modal, and PDN withdrawal use. Each flow runs against the `user_consents` Postgres table and emits one audit row.

## Public surface

| Method | Caller | Tx behaviour |
|---|---|---|
| `RecordRegistrationConsents` | `UserService.Register` | Runs inside the caller's tx — does NOT open its own |
| `ReConsent` | `POST /auth/consents` handler | Opens its own pgx tx, validates versions, UPSERTs, audits, commits |
| `WithdrawPDN` | `POST /users/me/consents/pdn/withdraw` handler | Calls `AccountDeletionService.RequestDeletion` first (its own tx), then opens a second tx for `MarkWithdrawn` + audit |
| `DiffAgainstCurrent` | `/auth/me` | Read-only; returns nil when no diffs |

## Version tracking

`CurrentVersionFunc(slug) (version, sha256)` is the build's current-version + sha256 lookup — typically a closure over `legalconfig` constants. Versions come from `legalconfig.CurrentVersion(slug)`; the sha256 is empty when the policy text loader isn't yet wired (the frontend computes it from the `.md` file in 22-02).

The three slugs are fixed: `tos`, `privacy`, `pdn` (see `legalconfig.AllSlugs()`). All three are bumped in lockstep by Phase 22, so the versions match across the slice on Register and ReConsent.

## Register flow — `RecordRegistrationConsents`

Loops over the submitted policies and calls `consents.UpsertConsent` for each, then writes ONE consolidated `audit.LogConsentRecordedTx` row. The handler validates that `policies` contains exactly the three expected slugs at the build's `currentVersion` BEFORE invoking — the service trusts the input shape and focuses on the persistence loop.

Runs INSIDE the caller's tx so the consent rows + user row commit together (atomic-Register invariant). The audit row carries the first policy's `(policyVersion, policySHA256)` pair as the canonical pair — all three are bumped in lockstep, so the values match across the slice.

## ReConsent modal — `ReConsent`

Validates every submitted `(slug, version)` matches `legalconfig.CurrentVersion(slug)`:

- Unknown slug (`wantVersion == ""`) → `domain.ErrConsentMissing` → handler returns 400.
- Stale version (operator bumped mid-review) → `domain.ErrConsentVersionMismatch` → handler returns 409.

After validation, opens a pgx tx, UPSERTs every row, writes `audit.LogConsentReconsentedTx`, commits. `defer func() { _ = tx.Rollback(ctx) }()` is the standard rollback-on-error pattern — a successful `Commit` makes the rollback a no-op.

`fromVersion` in the audit row is best-effort empty for v1.4 — no historical pre-v22 lookup is performed. Forensic reconstruction relies on the `user_consents` row's `policy_version` delta instead.

Empty `policies` slice fails fast with `ErrConsentMissing` before opening the tx.

## PDN withdrawal — `WithdrawPDN`

Trigger order is load-bearing:

1. **`AccountDeletionService.RequestDeletion(userID, "", ip, userAgent, "consent_withdrawn")`** — empty password skips the password-check (the consent-withdrawal flow is its own authentication path). Runs in its own tx inside the deletion service.
2. **`pool.BeginTx` → `consents.MarkWithdrawn` (purpose=pdn) → `audit.LogConsentWithdrawnTx` → `tx.Commit`** — sets `user_consents.withdrawn_at = NOW()` for the pdn row and writes the audit row in the SAME tx.

`ErrDeletionAlreadyPending` surfaces unchanged from step 1 so the handler returns 423 Locked.

The two-tx split is intentional: account deletion has its own atomic invariants (refresh-token revocation, deletion warning enqueue) that must commit independently of the consent row. If the deletion succeeds but the withdrawn-at write fails, the user is already on the deletion track — the consent row's withdrawn_at flag becomes irrelevant once the user record is hard-deleted at T+7d.

## Re-consent prompts — `DiffAgainstCurrent`

Reads the user's three rows via `ListByUser`, indexes them by `Purpose`, then iterates `legalconfig.AllSlugs()` and produces a `PolicyDiff` for every slug whose recorded `PolicyVersion` differs from the build's `currentVersion`.

Two special cases:

- **No row at all** → diff with `OldVersion = ""`. Pre-v22 backfill missing this purpose, or the user predates the table. Trigger re-consent.
- **`WithdrawnAt != nil`** → skip. The user withdrew this consent and is mid-deletion; the modal would be useless.

Returns `(nil, nil)` when no diffs — `omitempty` on the parent field then surfaces `"requiresReconsent": null` in `/auth/me`.

## Audit emission

Three audit functions in `pkg/audit` are called, one per flow:

| Function | Caller | Tx |
|---|---|---|
| `LogConsentRecordedTx(ctx, tx, userID, purposes, version, sha256, ip, ua)` | `RecordRegistrationConsents` | Caller's tx |
| `LogConsentReconsentedTx(ctx, tx, userID, purposes, fromVersion, toVersion, ip, ua)` | `ReConsent` | Service-opened tx |
| `LogConsentWithdrawnTx(ctx, tx, userID, slug, ip, ua)` | `WithdrawPDN` | Service-opened tx |

All three are tx-scoped variants — the audit row commits atomically with the consent row(s) it describes. There is no async path for consent audit: legal forensics require the audit row to be definitively present whenever the consent state changed.

## Data shapes

```go
type PolicyAccepted struct {
    Slug    string // "tos" | "privacy" | "pdn"
    Version string
    SHA256  string // may be empty (frontend computes from .md file in 22-02)
}

type PolicyDiff struct {
    Slug       string `json:"slug"`       // "tos" | "privacy" | "pdn"
    OldVersion string `json:"oldVersion"` // "" when no row exists (pre-v22)
    NewVersion string `json:"newVersion"`
    SHA256     string `json:"sha256"`
}

type RequiresReconsentInfo struct {
    Policies []PolicyDiff `json:"policies"`
}
```

`RequiresReconsentInfo` is the `/auth/me` payload returned when at least one policy is stale.

## Dependency seams

`ConsentRepo` and `AccountDeletionForConsent` are declared as interfaces so unit tests can substitute mocks. The production implementations are `*repository.UserConsentsRepository` and `*AccountDeletionService`. `CurrentVersionFunc` is a function-typed seam over `legalconfig` so the wire layer can inject test versions without touching the package globals.

## Cross-references

- `pkg/legalconfig` — policy version constants and `AllSlugs()`.
- `pkg/audit` — `LogConsentRecordedTx`, `LogConsentReconsentedTx`, `LogConsentWithdrawnTx`.
- `docs/services/account-deletion.md` — withdrawal trigger downstream.
- `services/api/migrations/` — `user_consents` table schema.
