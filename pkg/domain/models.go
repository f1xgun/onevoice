package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID    uuid.UUID `json:"id" db:"id"`
	Email string    `json:"email" db:"email"`
	// Name is the user's display name, captured at registration and editable
	// via PATCH /auth/profile. NOT NULL DEFAULT '' in the schema, so pre-feature
	// rows read back as "" rather than NULL.
	Name         string `json:"name" db:"name"`
	PasswordHash string `json:"-" db:"password_hash"`
	// PreferredLocale is the user's chosen UI language ('ru' | 'en').
	// Persisted in users.preferred_locale. DB default 'ru'; CHECK constraint
	// enforces the two-value enum. The frontend reads this on /auth/me to
	// seed the locale cookie and writes it via PATCH /auth/locale.
	PreferredLocale string `json:"preferred_locale" db:"preferred_locale"`
	// EmailVerified — / /. JSON-hidden by default;
	// the /auth/me handler surfaces it via a wrapper struct (MeResponse) so
	// other endpoints listing users don't accidentally leak the flag.
	EmailVerified bool `json:"-" db:"email_verified"`
	// EmailVerifiedAt is set when the user POSTs the verify-email link.
	// JSON-hidden — surfaced only via /auth/me's wrapper.
	EmailVerifiedAt *time.Time `json:"-" db:"email_verified_at"`
	// account-deletion lifecycle. All three are
	// pointer-time.Time so they can be nil (the "no pending deletion" state).
	// JSON-hidden — /auth/me exposes them via a typed accountDeletion field
	// on MeResponse so other endpoints listing users don't accidentally
	// leak them. GetByID / GetByEmail filter `deleted_at IS NULL` so a
	// soft-deleted user becomes "not found" everywhere reads happen;
	// use GetByIDIncludingDeleted when the deletion-aware code path needs to
	// inspect these fields.
	DeletedAt           *time.Time `json:"-" db:"deleted_at"`
	DeletionRequestedAt *time.Time `json:"-" db:"deletion_requested_at"`
	DeletionCanceledAt  *time.Time `json:"-" db:"deletion_canceled_at"`
	CreatedAt           time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt           time.Time  `json:"updatedAt" db:"updated_at"`
}

type Business struct {
	HasFirstSuccessfulAction bool                   `json:"hasFirstSuccessfulAction" db:"-"`
	ID                       uuid.UUID              `json:"id" db:"id"`
	Name                     string                 `json:"name" db:"name"`
	Category                 string                 `json:"category" db:"category"`
	Address                  string                 `json:"address" db:"address"`
	Phone                    string                 `json:"phone" db:"phone"`
	Website                  *string                `json:"website" db:"website"`
	Description              string                 `json:"description" db:"description"`
	LogoURL                  string                 `json:"logoUrl" db:"logo_url"`
	Settings                 map[string]interface{} `json:"settings" db:"settings"`
	// organization-deletion lifecycle. All three are pointer-time.Time so they
	// can be nil (the "no pending deletion" state). JSON-hidden — surfaced via a
	// typed wrapper at the read boundary so listing endpoints don't leak them.
	// GetByID filters `deleted_at IS NULL` so a soft-deleted organization
	// becomes "not found" everywhere reads happen; use GetByIDIncludingDeleted
	// when the deletion-aware code path needs to inspect these fields.
	DeletedAt           *time.Time `json:"-" db:"deleted_at"`
	DeletionRequestedAt *time.Time `json:"-" db:"deletion_requested_at"`
	DeletionCanceledAt  *time.Time `json:"-" db:"deletion_canceled_at"`
	CreatedAt           time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt           time.Time  `json:"updatedAt" db:"updated_at"`
}

type BusinessSchedule struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	BusinessID  uuid.UUID  `json:"businessId" db:"business_id"`
	DayOfWeek   int        `json:"dayOfWeek" db:"day_of_week"`
	OpenTime    string     `json:"openTime" db:"open_time"`
	CloseTime   string     `json:"closeTime" db:"close_time"`
	IsClosed    bool       `json:"isClosed" db:"is_closed"`
	SpecialDate *time.Time `json:"specialDate,omitempty" db:"special_date"`
}

type Integration struct {
	ID                    uuid.UUID              `json:"id" db:"id"`
	BusinessID            uuid.UUID              `json:"businessId" db:"business_id"`
	Platform              string                 `json:"platform" db:"platform"`
	Status                string                 `json:"status" db:"status"`
	EncryptedAccessToken  []byte                 `json:"-" db:"encrypted_access_token"`
	EncryptedRefreshToken []byte                 `json:"-" db:"encrypted_refresh_token"`
	EncryptedUserToken    []byte                 `json:"-" db:"encrypted_user_token"`
	ExternalID            string                 `json:"externalId" db:"external_id"`
	Metadata              map[string]interface{} `json:"metadata" db:"metadata"`
	TokenExpiresAt        *time.Time             `json:"tokenExpiresAt,omitempty" db:"token_expires_at"`
	UserTokenExpiresAt    *time.Time             `json:"-" db:"user_token_expires_at"`
	CreatedAt             time.Time              `json:"createdAt" db:"created_at"`
	UpdatedAt             time.Time              `json:"updatedAt" db:"updated_at"`
	// WrappedDEK is the KMS-wrapped per-row DEK; nil for legacy rows not yet rekeyed.
	WrappedDEK []byte `json:"-" db:"wrapped_dek"`
	// KeyVersion is the int16 identifying the KMS key version that wrapped this row's DEK; 0 = legacy/unset.
	KeyVersion int16 `json:"-" db:"key_version"`
	// EncryptionKeyFingerprint is the SHA-256 hex of TOKEN_ENCRYPTION_KMS_KEY_ID at encrypt time; "" = legacy.
	EncryptionKeyFingerprint string `json:"-" db:"encryption_key_fingerprint"`
}

// SyncState is the proactive platform-sync reconciliation record for one
// (business, platform, external_id) tuple. It records when a connected channel
// was last compared against the stored business profile, whether the platform
// copy has drifted from what OneVoice pushed, the exponential-backoff schedule,
// and the last fetched remote snapshot.
//
// LastRemoteSnapshot may transiently hold PII (e.g. a business phone read back
// from Yandex). It is erased on business hard-delete via the sync_state →
// businesses ON DELETE CASCADE and MUST be redacted out of any log line.
type SyncState struct {
	ID                  uuid.UUID         `json:"id" db:"id"`
	BusinessID          uuid.UUID         `json:"businessId" db:"business_id"`
	Platform            string            `json:"platform" db:"platform"`
	ExternalID          string            `json:"externalId" db:"external_id"`
	LastCheckedAt       *time.Time        `json:"lastCheckedAt,omitempty" db:"last_checked_at"`
	LastRemoteSnapshot  map[string]string `json:"-" db:"last_remote_snapshot"`
	DriftDetected       bool              `json:"driftDetected" db:"drift_detected"`
	DriftFields         []string          `json:"driftFields" db:"drift_fields"`
	ConsecutiveFailures int               `json:"-" db:"consecutive_failures"`
	LastError           string            `json:"-" db:"last_error"`
	NextCheckAt         time.Time         `json:"nextCheckAt" db:"next_check_at"`
	CreatedAt           time.Time         `json:"createdAt" db:"created_at"`
	UpdatedAt           time.Time         `json:"updatedAt" db:"updated_at"`
}

// Subscription is one business's billing plan. The v1.6 reshape moves the
// tenant key from user_id (legacy, dead) to business_id — usage_logs and the
// whole billing model are business-scoped. ParentBusinessID (agency seam),
// Provider / ProviderSubID (payment-provider seam) and CancelAtPeriodEnd are
// Track-B columns: nullable and never written in Track-A.
type Subscription struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	BusinessID       uuid.UUID  `json:"businessId" db:"business_id"`
	ParentBusinessID *uuid.UUID `json:"parentBusinessId,omitempty" db:"parent_business_id"`
	PlanCode         string     `json:"planCode" db:"plan_code"`
	Status           string     `json:"status" db:"status"`
	PeriodStart      *time.Time `json:"periodStart,omitempty" db:"period_start"`
	PeriodEnd        *time.Time `json:"periodEnd,omitempty" db:"period_end"`
	// Track-B payment-provider seams. Nil in Track-A.
	Provider          *string   `json:"-" db:"provider"`
	ProviderSubID     *string   `json:"-" db:"provider_sub_id"`
	CancelAtPeriodEnd bool      `json:"cancelAtPeriodEnd" db:"cancel_at_period_end"`
	CreatedAt         time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt         time.Time `json:"updatedAt" db:"updated_at"`
}

// Subscription status values (subscriptions.status CHECK constraint).
const (
	SubscriptionStatusActive   = "active"
	SubscriptionStatusPastDue  = "past_due"
	SubscriptionStatusCanceled = "canceled"
	SubscriptionStatusExpired  = "expired"
)

// PlanDefinition is one row of the plan_definitions catalog (Free / Pro /
// Enterprise). Numeric fields carry placeholder values until the founder sets
// real numbers via a follow-up migration. A -1 in DailyLLMUSDCap /
// MaxIntegrations / MaxMembers means "unlimited".
type PlanDefinition struct {
	Code                     string    `json:"code" db:"code"`
	DisplayName              string    `json:"displayName" db:"display_name"`
	PriceRUB                 float64   `json:"priceRub" db:"price_rub"`
	MonthlyCredits           int       `json:"monthlyCredits" db:"monthly_credits"`
	OveragePricePerCreditRUB float64   `json:"overagePricePerCreditRub" db:"overage_price_per_credit_rub"`
	DailyLLMUSDCap           float64   `json:"dailyLlmUsdCap" db:"daily_llm_usd_cap"`
	MaxIntegrations          int       `json:"maxIntegrations" db:"max_integrations"`
	MaxMembers               int       `json:"maxMembers" db:"max_members"`
	RateLimitTier            string    `json:"rateLimitTier" db:"rate_limit_tier"`
	Active                   bool      `json:"active" db:"active"`
	SortOrder                int       `json:"sortOrder" db:"sort_order"`
	CreatedAt                time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt                time.Time `json:"updatedAt" db:"updated_at"`
}

// CreditLedgerEntry is one append-only row of credit_ledger. The running
// balance is the BalanceAfter of the most-recent row per business; each row is
// immutable. IdempotencyKey is set to the usage-log id on metered rows so a
// retried metering write is a no-op (ON CONFLICT DO NOTHING).
type CreditLedgerEntry struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	BusinessID         uuid.UUID  `json:"businessId" db:"business_id"`
	DeltaCredits       int        `json:"deltaCredits" db:"delta_credits"`
	BalanceAfter       int        `json:"balanceAfter" db:"balance_after"`
	OverageCredits     int        `json:"overageCredits" db:"overage_credits"`
	Reason             string     `json:"reason" db:"reason"`
	UsageLogID         *uuid.UUID `json:"usageLogId,omitempty" db:"usage_log_id"`
	SubscriptionPeriod *string    `json:"subscriptionPeriod,omitempty" db:"subscription_period"`
	IdempotencyKey     *string    `json:"-" db:"idempotency_key"`
	CreatedAt          time.Time  `json:"createdAt" db:"created_at"`
}

// Credit-ledger reason values (credit_ledger.reason CHECK constraint).
const (
	CreditReasonGrant   = "grant"
	CreditReasonConsume = "consume"
	CreditReasonOverage = "overage"
	CreditReasonRefund  = "refund"
	CreditReasonExpire  = "expire"
)

// AuditLog is the persisted record of a security-sensitive mutation.
//
// BusinessID is nullable for system-wide events (e.g. failed login before
// the user is known). UserID is nullable for the same reason: a failed
// login attempt against a non-existent or wrong-password account has no
// resolvable user.
//
// Details is stored as raw JSONB; typed constructors live in pkg/audit and
// marshal their own typed Details structs into this field. Callers must
// not write map[string]interface{}.
type AuditLog struct {
	ID         uuid.UUID       `json:"id" db:"id"`
	BusinessID *uuid.UUID      `json:"businessId" db:"business_id"`
	UserID     *uuid.UUID      `json:"userId" db:"user_id"`
	Action     string          `json:"action" db:"action"`
	Resource   string          `json:"resource" db:"resource"`
	Details    json.RawMessage `json:"details" db:"details"`
	// UserEmailAtEvent is the actor's email captured at write-time by
	// pkg/audit/logger.go via a UserResolver lookup. / :
	// after 's hard-delete fires, user_id may be NULL (FK SET
	// NULL) but this column preserves identity so 152-ФЗ audit queries
	// still resolve the actor. Empty string when UserID is nil OR the
	// resolver returned an error (failure NEVER blocks the audit row).
	UserEmailAtEvent string    `json:"userEmailAtEvent,omitempty" db:"user_email_at_event"`
	CreatedAt        time.Time `json:"createdAt" db:"created_at"`
}

type RefreshToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// ToolApprovals extracts the typed tool-approval overrides from
// the generic Business.Settings map. The storage format inside Settings is:
//
//	settings["tool_approvals"] = map[string]interface{}{
//
// "telegram__send_channel_post": "manual",
// "vk__send_post":               "auto",
// ...
//
//	}
//
// Returns a non-nil empty map when Settings is nil, when the tool_approvals
// key is missing, or when its value is not a map. Invalid enum values (i.e.
// anything outside ValidToolFloor) are skipped silently — the safe default
// behavior is "fall back to the registry floor", which downstream
// hitl.Resolve achieves automatically when a key is absent.
//
// Keeping the parsing in one place prevents divergent interpretation of
// malformed data between the orchestrator (pause time) and the API (resolve
// time TOCTOU re-check).
func (b Business) ToolApprovals() map[string]ToolFloor {
	out := make(map[string]ToolFloor)
	raw, ok := b.Settings["tool_approvals"].(map[string]interface{})
	if !ok {
		return out
	}
	for name, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		f := ToolFloor(s)
		if !ValidToolFloor(f) {
			continue
		}
		out[name] = f
	}
	return out
}
