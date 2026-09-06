package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/oauthlock"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// memberStatusSuspended is the business_members.status value that bars a member
// from acting; it mirrors the "suspended" branch authz.RequireBusinessAccess
// rejects with 403.
const memberStatusSuspended = "suspended"

// TokenRefresher abstracts the HTTP call to refresh an expired OAuth token.
// See docs/services/integration.md.
type TokenRefresher interface {
	// RefreshToken exchanges a refresh token for a new access token (newRefreshToken empty if not rotated).
	RefreshToken(ctx context.Context, refreshToken string) (accessToken string, newRefreshToken string, expiresIn int64, err error)
}

// ConnectParams holds parameters for connecting a new platform integration.
// See docs/services/integration.md.
type ConnectParams struct {
	BusinessID       uuid.UUID
	ActorID          uuid.UUID
	Platform         string
	ExternalID       string
	AccessToken      string
	RefreshToken     string
	UserToken        string     // VK user token for read operations (optional)
	UserTokenExpires *time.Time // VK user token expiration (optional)
	Metadata         map[string]interface{}
	ExpiresAt        *time.Time
	ActorIP          string
	UserAgent        string
	ParsedFormat     string
}

// ParsedFormat values for ConnectParams.ParsedFormat record how a credential
// was supplied, for forensic provenance on the integration.connected audit row.
const (
	ParsedFormatBotToken    = "bot_token"
	ParsedFormatAccessToken = "access_token"
	ParsedFormatOAuthCode   = "oauth_code"
)

// callerServiceInternal is the caller_service recorded on the token-decrypt
// audit row when the request carries no mTLS service identity (in-process call).
const callerServiceInternal = "api.internal"

// TokenResponse holds decrypted token data for a platform integration.
type TokenResponse struct {
	IntegrationID    uuid.UUID              `json:"integration_id"`
	Platform         string                 `json:"platform"`
	ExternalID       string                 `json:"external_id"`
	AccessToken      string                 `json:"access_token"`
	UserToken        string                 `json:"user_token,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt        *time.Time             `json:"expires_at,omitempty"`
	UserTokenExpires *time.Time             `json:"user_token_expires_at,omitempty"`
}

// NATSPublisher is the narrow publish surface the integration service needs to
// fan out a revoke on delete. *nats.Conn satisfies it directly.
type NATSPublisher interface {
	Publish(subject string, data []byte) error
}

// ActorLookup is the narrow slice of domain.UserRepository the connect-actor
// gate needs. Connect uses it to re-assert the same email-verified and
// account-deletion-grace predicates the RequireVerifiedEmailDay0 and
// BlockWritesDuringGrace middlewares enforce, so credential-persisting entry
// paths that sit outside those middlewares (the public OAuth callbacks) cannot
// bypass the gates. It reads the actor INCLUDING soft-deleted rows
// (GetByIDIncludingDeleted): RequestDeletion stamps both deletion_requested_at
// and deleted_at, so a user inside the grace window is soft-deleted and the
// `deleted_at IS NULL`-filtered GetByID would miss it — failing the gate open.
type ActorLookup interface {
	GetByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

// MembershipChecker is the narrow slice of the authz cache the connect path
// needs to re-assert that the acting user is still an ACTIVE member of the
// target business holding integrations:connect. *authz.Cache satisfies it.
//
// The synchronous paste-flow connect routes run behind
// authz.RequireBusinessAccess + authz.Can(PermIntegrationsConnect), so this is
// checked at request entry there. The asynchronous OAuth code-flow callbacks
// are PUBLIC routes whose only authz proof is the single-use OAuth state minted
// ~10 min earlier; a member removed (or whose connect permission is revoked)
// inside that window would otherwise still bind an integration. Re-running the
// same membership + permission lookup against the live cache at Connect time
// closes that freshness gap and is redundant-but-harmless on the paste-flow
// path (a current member always passes).
type MembershipChecker interface {
	GetMembership(ctx context.Context, businessID, userID uuid.UUID) (authz.CachedMember, error)
	GetRole(ctx context.Context, roleID uuid.UUID) (authz.CachedRole, error)
}

// BusinessLookup is the narrow slice of domain.BusinessRepository the connect
// path needs to re-assert that the target business is still live before a fresh
// token is persisted. It reads through the soft-delete-aware GetByID
// (deleted_at IS NULL), which surfaces a soft-deleted (pending-erasure)
// organization as domain.ErrBusinessNotFound.
//
// authz.RequireBusinessAccess gates the /businesses/{id} routes purely on the
// business_members membership row; it does NOT re-load the business through the
// soft-delete-aware read, so during the deletion grace window a member can still
// reach Connect and bind new personal data (envelope-encrypted OAuth/bot tokens)
// to an organization awaiting erasure. Re-checking existence here — at the single
// choke point every platform connect funnels through — blocks that new-PII
// ingestion without regressing /restore or reads, which never reach Connect.
type BusinessLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
}

// IntegrationService defines the interface for platform integration management.
// See docs/services/integration.md.
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID, actorID uuid.UUID) error

	Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*TokenResponse, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
	UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error
	MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) error
	SetSharedSession(ctx context.Context, params SharedSessionParams) (*domain.Integration, error)
}

// SharedSessionParams sets/rotates the single KMS-wrapped shared representative
// session credential stored under the reserved sentinel row (sharedBusinessID +
// platform + external_id "__shared_rep__"). It is the ops-bootstrap path for
// the delegated-representative access plane and deliberately bypasses the
// per-actor connect gates: it is admin-gated at the handler, operates only on
// the reserved sentinel coordinates, and the sentinel business is not a
// customer organization.
type SharedSessionParams struct {
	SharedBusinessID uuid.UUID
	Platform         string
	// Credential is the session cookie JSON (or OAuth token) for the shared
	// representative account.
	Credential string
	ActorIP    string
	UserAgent  string
}

type integrationService struct {
	repo      domain.IntegrationRepository
	envelope  *crypto.Envelope
	pool      oauthlock.LockExecutor
	refreshMu sync.Map       // map[uuid.UUID]*sync.Mutex — per-integration refresh lock (legacy path fallback)
	refresher TokenRefresher // nil for platforms that don't need refresh
	audit     audit.Logger
	nats      NATSPublisher     // nil when NATS is unreachable — revoke publish is skipped (fail-open)
	actors    ActorLookup       // nil disables the connect-actor gate (in-process callers already gated upstream)
	members   MembershipChecker // nil disables the connect-membership gate (in-process callers already gated upstream)
	business  BusinessLookup    // nil disables the connect-business-existence gate (in-process callers pass a live business)
}

// Compile-time check that integrationService implements IntegrationService
var _ IntegrationService = (*integrationService)(nil)

// NewIntegrationService constructs the service.
// envelope wraps the KMS-backed per-row DEK encryption; it falls through to
// the legacy Encryptor for rows not yet rekeyed (WrappedDEK IS NULL).
// pool is the Postgres connection pool used for OAuth advisory locking.
// refresher/auditLogger may be nil (auto-defaulted to Nop).
// See docs/services/integration.md.
func NewIntegrationService(repo domain.IntegrationRepository, envelope *crypto.Envelope, pool oauthlock.LockExecutor, refresher TokenRefresher, auditLogger audit.Logger) IntegrationService {
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &integrationService{
		repo:      repo,
		envelope:  envelope,
		pool:      pool,
		refresher: refresher,
		audit:     auditLogger,
	}
}

// WithNATSPublisher attaches the publisher used to fan out a revoke when an
// integration is deleted, returning svc for chaining. When the publisher is
// unset (or nil), or svc is not the concrete service, Delete skips the publish
// and relies on the cache TTL backstop. Constructed separately from the
// constructor so existing call sites that do not need fan-out are unaffected.
func WithNATSPublisher(svc IntegrationService, p NATSPublisher) IntegrationService {
	if s, ok := svc.(*integrationService); ok {
		s.nats = p
	}
	return svc
}

// WithActorGate attaches the user lookup that lets Connect re-assert the
// email-verified and account-deletion-grace predicates against the acting
// user, returning svc for chaining. When the lookup is unset (or nil), or svc
// is not the concrete service, Connect skips the gate — safe for callers that
// are already gated by the RequireVerifiedEmailDay0 / BlockWritesDuringGrace
// middlewares. Attached so the public OAuth callbacks, which persist a live
// integration outside those middlewares, cannot bypass them. The check is
// idempotent on already-gated paste-flow routes (they reject upstream before
// Connect is reached). Constructed separately from the constructor so existing
// call sites that do not need the gate are unaffected.
func WithActorGate(svc IntegrationService, actors ActorLookup) IntegrationService {
	if s, ok := svc.(*integrationService); ok {
		s.actors = actors
	}
	return svc
}

// WithMembershipGate attaches the authz membership/permission source that lets
// Connect re-assert, against the live cache, that the acting user is still an
// active member of the target business holding integrations:connect, returning
// svc for chaining. When the checker is unset (or nil), or svc is not the
// concrete service, Connect skips the membership re-check — safe for callers
// already gated by authz.RequireBusinessAccess + authz.Can. Attached so the
// public OAuth callbacks, whose only authz proof is a ~10-min-old OAuth state,
// cannot bind an integration for an actor removed (or whose connect permission
// was revoked) inside that window. Mirrors WithActorGate's wiring pattern.
func WithMembershipGate(svc IntegrationService, members MembershipChecker) IntegrationService {
	if s, ok := svc.(*integrationService); ok {
		s.members = members
	}
	return svc
}

// WithBusinessGate attaches the soft-delete-aware business lookup that lets
// Connect reject an integration bound to an organization awaiting erasure,
// returning svc for chaining. When the lookup is unset (or nil), or svc is not
// the concrete service, Connect skips the existence gate. Attached so a member
// acting inside the deletion grace window — whose membership row still satisfies
// authz.RequireBusinessAccess — cannot persist fresh envelope-encrypted tokens
// to a soft-deleted business. Mirrors WithMembershipGate's wiring pattern.
func WithBusinessGate(svc IntegrationService, business BusinessLookup) IntegrationService {
	if s, ok := svc.(*integrationService); ok {
		s.business = business
	}
	return svc
}

// getRefreshMutex returns a per-integration mutex for serializing refresh calls
// on the legacy (non-oauthlock) path.
func (s *integrationService) getRefreshMutex(id uuid.UUID) *sync.Mutex {
	val, _ := s.refreshMu.LoadOrStore(id, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// refresherSupports reports whether the wired refresher may be applied to the
// given platform. The configured refresher is the Google OAuth refresher, which
// POSTs to Google's token endpoint with the Google client_id/secret; applying
// it to a non-Google row would ship that row's refresh token to Google. Other
// platforms (e.g. yandex_business, which also persists a refresh token + expiry)
// must not borrow it — they fall through to ErrTokenExpired until their own
// refresher is wired.
func (s *integrationService) refresherSupports(platform string) bool {
	return platform == a2a.AgentGoogleBusiness
}

// ListByBusinessID retrieves all integrations for a business
func (s *integrationService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	integrations, err := s.repo.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}

	return integrations, nil
}

// GetByBusinessAndPlatform retrieves a specific integration by business and platform
func (s *integrationService) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}

	integration, err := s.repo.GetByBusinessAndPlatform(ctx, businessID, platform)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get integration: %w", err)
	}

	return integration, nil
}

// UpdateMetadata replaces only the metadata jsonb via a targeted single-column
// UPDATE; status, token, and envelope columns are left untouched so a concurrent
// status flip is never reverted by a stale read-modify-write snapshot.
// See docs/services/integration.md.
func (s *integrationService) UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	integ, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}

	if err := s.repo.UpdateMetadata(ctx, integrationID, metadata); err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("update integration: %w", err)
	}

	audit.LogIntegrationMetadataUpdated(ctx, s.audit, integ.BusinessID, integrationID, integ.Platform, sortedKeys(metadata))
	return nil
}

// sortedKeys returns the map's keys in deterministic sorted order for a stable
// audit payload. Values are never included — keys only.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// UpdateExternalID heals the external_id post-connect (e.g. Yandex Sprav
// permalink resolved later) via a targeted single-column UPDATE; status, token,
// and envelope columns are left untouched so a concurrent status flip is never
// reverted by a stale read-modify-write snapshot.
// See docs/services/integration.md.
func (s *integrationService) UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}
	if externalID == "" {
		return fmt.Errorf("external id is required")
	}

	integ, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}

	if err := s.repo.UpdateExternalID(ctx, integrationID, externalID); err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("update integration: %w", err)
	}

	audit.LogIntegrationExternalIDUpdated(ctx, s.audit, integ.BusinessID, integrationID, integ.Platform, integ.ExternalID, externalID)
	return nil
}

// Delete soft-deletes an integration, emits an integration.deleted audit row,
// and fans out a revoke on integrations.revoked.{platform}.{businessID} so live
// agent caches invalidate the token. The publish is fail-open: a publish error
// is logged + metered but never blocks the deletion (the cache TTL is the
// backstop). actorID identifies the user who performed the deletion.
func (s *integrationService) Delete(ctx context.Context, integrationID, actorID uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	integ, err := s.repo.GetByID(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("get integration: %w", err)
	}

	if err := s.repo.SoftDelete(ctx, integrationID); err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("soft-delete: %w", err)
	}

	audit.LogIntegrationDeleted(ctx, s.audit, integ.BusinessID, actorID, integrationID, integ.Platform, integ.ExternalID)

	if s.nats != nil {
		subject := fmt.Sprintf("integrations.revoked.%s.%s", integ.Platform, integ.BusinessID.String())
		if err := s.nats.Publish(subject, []byte("{}")); err != nil {
			slog.WarnContext(ctx, "revoke publish failed; cache TTL backstop will handle",
				"subject", subject, "error", err)
			metrics.IncIntegrationsRevokePublishFailed()
		}
	}

	return nil
}

// Connect creates a new platform integration, encrypting tokens via envelope
// encryption (KMS-wrapped per-row DEK) before storage.
// A UUID is allocated server-side before encryption so that the AAD binding
// (integrationID + platform) matches the row's eventual primary key.
func (s *integrationService) Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error) {
	if params.BusinessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}
	if params.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}

	if err := s.gateActor(ctx, params.ActorID); err != nil {
		return nil, err
	}

	if err := s.gateMembership(ctx, params.ActorID, params.BusinessID); err != nil {
		return nil, err
	}

	if err := s.gateBusiness(ctx, params.BusinessID); err != nil {
		return nil, err
	}

	if params.ExternalID != "" {
		claimant, claimErr := s.repo.GetActiveByPlatformExternal(ctx, params.Platform, params.ExternalID)
		switch {
		case claimErr == nil:
			if claimant.BusinessID != params.BusinessID {
				return nil, domain.ErrIntegrationClaimedByOtherTenant
			}
		case errors.Is(claimErr, domain.ErrIntegrationNotFound):
		default:
			return nil, fmt.Errorf("lookup cross-tenant integration: %w", claimErr)
		}

		existing, lookupErr := s.repo.GetByBusinessPlatformExternal(ctx, params.BusinessID, params.Platform, params.ExternalID)
		switch {
		case lookupErr == nil:
			if delErr := s.repo.SoftDelete(ctx, existing.ID); delErr != nil && !errors.Is(delErr, domain.ErrIntegrationNotFound) {
				return nil, fmt.Errorf("retire existing integration: %w", delErr)
			}
		case errors.Is(lookupErr, domain.ErrIntegrationNotFound):
		default:
			return nil, fmt.Errorf("lookup existing integration: %w", lookupErr)
		}
	}

	integrationID := uuid.New()

	plaintexts := [][]byte{
		[]byte(params.AccessToken),
		[]byte(params.RefreshToken),
		[]byte(params.UserToken),
	}

	ciphertexts, wrappedDEK, keyVersion, fingerprint, err := s.envelope.EncryptForRow(ctx, integrationID, params.Platform, plaintexts)
	if err != nil {
		return nil, fmt.Errorf("envelope encrypt: %w", err)
	}

	var encAccess, encRefresh, encUser []byte
	if params.AccessToken != "" {
		encAccess = ciphertexts[0]
	}
	if params.RefreshToken != "" {
		encRefresh = ciphertexts[1]
	}
	if params.UserToken != "" {
		encUser = ciphertexts[2]
	}

	metadata := params.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	integration := &domain.Integration{
		ID:                       integrationID,
		BusinessID:               params.BusinessID,
		Platform:                 params.Platform,
		Status:                   domain.IntegrationStatusActive,
		ExternalID:               params.ExternalID,
		EncryptedAccessToken:     encAccess,
		EncryptedRefreshToken:    encRefresh,
		EncryptedUserToken:       encUser,
		WrappedDEK:               wrappedDEK,
		KeyVersion:               keyVersion,
		EncryptionKeyFingerprint: fingerprint,
		Metadata:                 metadata,
		TokenExpiresAt:           params.ExpiresAt,
		UserTokenExpiresAt:       params.UserTokenExpires,
	}

	if err := s.repo.Create(ctx, integration); err != nil {
		return nil, err
	}

	audit.LogIntegrationConnected(ctx, s.audit, params.BusinessID, params.ActorID, integration.ID, params.Platform, params.ExternalID, params.ActorIP, params.UserAgent, params.ParsedFormat)

	return integration, nil
}

// SetSharedSession sets or rotates the shared representative session credential
// stored under the reserved sentinel row. It retires any existing sentinel row
// (soft delete) and creates a fresh envelope-encrypted one, exactly like a
// Connect against the sentinel coordinates but WITHOUT the per-actor / business
// gates (the sentinel is not a customer org and the caller is admin-gated
// upstream). The credential is stored as the row's access token; the row is the
// single source the agent's GetSharedSession decrypts.
func (s *integrationService) SetSharedSession(ctx context.Context, params SharedSessionParams) (*domain.Integration, error) {
	if params.SharedBusinessID == uuid.Nil {
		return nil, fmt.Errorf("shared business id is required")
	}
	if params.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	if params.Credential == "" {
		return nil, fmt.Errorf("shared session credential is required")
	}

	externalID := tools.YandexSharedRepExternalID

	existing, lookupErr := s.repo.GetByBusinessPlatformExternal(ctx, params.SharedBusinessID, params.Platform, externalID)
	switch {
	case lookupErr == nil:
		if delErr := s.repo.SoftDelete(ctx, existing.ID); delErr != nil && !errors.Is(delErr, domain.ErrIntegrationNotFound) {
			return nil, fmt.Errorf("retire existing shared session: %w", delErr)
		}
	case errors.Is(lookupErr, domain.ErrIntegrationNotFound):
	default:
		return nil, fmt.Errorf("lookup existing shared session: %w", lookupErr)
	}

	integrationID := uuid.New()
	plaintexts := [][]byte{
		[]byte(params.Credential),
		nil,
		nil,
	}
	ciphertexts, wrappedDEK, keyVersion, fingerprint, err := s.envelope.EncryptForRow(ctx, integrationID, params.Platform, plaintexts)
	if err != nil {
		return nil, fmt.Errorf("envelope encrypt shared session: %w", err)
	}

	integration := &domain.Integration{
		ID:                       integrationID,
		BusinessID:               params.SharedBusinessID,
		Platform:                 params.Platform,
		Status:                   domain.IntegrationStatusActive,
		ExternalID:               externalID,
		EncryptedAccessToken:     ciphertexts[0],
		WrappedDEK:               wrappedDEK,
		KeyVersion:               keyVersion,
		EncryptionKeyFingerprint: fingerprint,
		Metadata: map[string]interface{}{
			"shared_representative": true,
			"connected_at":          time.Now().UTC().Format(time.RFC3339),
		},
	}

	if err := s.repo.Create(ctx, integration); err != nil {
		return nil, err
	}

	audit.LogIntegrationConnected(ctx, s.audit, params.SharedBusinessID, uuid.Nil, integration.ID, params.Platform, externalID, params.ActorIP, params.UserAgent, ParsedFormatAccessToken)

	return integration, nil
}

// gateActor re-asserts, against the acting user, the same predicates the
// RequireVerifiedEmailDay0 and BlockWritesDuringGrace middlewares enforce:
// reject when the email is unverified (ErrActorEmailNotVerified) or when the
// account is inside the deletion grace window — deletion_requested_at set and
// deletion_canceled_at null (ErrActorPendingDeletion). The lookup reads the
// actor including soft-deleted rows, so a grace-window user (whose deleted_at
// is stamped alongside deletion_requested_at) is still found and rejected. It
// is a no-op when no ActorLookup is attached (in-process callers already gated
// upstream) or when actorID is the nil UUID (no identified actor to gate). A
// genuinely missing user row is treated as not-gated rather than a hard error,
// mirroring the middlewares' fail-open posture on an absent record.
func (s *integrationService) gateActor(ctx context.Context, actorID uuid.UUID) error {
	if s.actors == nil || actorID == uuid.Nil {
		return nil
	}
	u, err := s.actors.GetByIDIncludingDeleted(ctx, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil
		}
		return fmt.Errorf("load connect actor: %w", err)
	}
	if !u.EmailVerified {
		return domain.ErrActorEmailNotVerified
	}
	if u.DeletionRequestedAt != nil && u.DeletionCanceledAt == nil {
		return domain.ErrActorPendingDeletion
	}
	return nil
}

// gateMembership re-asserts, against the live authz cache, the same
// membership + permission predicates authz.RequireBusinessAccess +
// authz.Can(PermIntegrationsConnect) enforce at the synchronous paste-flow
// entry: the actor must be a member of businessID whose status is not
// suspended and whose role grants integrations:connect. It rejects with
// domain.ErrForbidden otherwise, before any integration row is created. It is a
// no-op when no MembershipChecker is attached (in-process callers already gated
// upstream) or when either id is the nil UUID (no identified actor/business to
// gate). A missing-membership lookup result is treated as a forbidden actor,
// not a hard error, so a revoked member on the async OAuth-callback path is
// rejected rather than failing open.
func (s *integrationService) gateMembership(ctx context.Context, actorID, businessID uuid.UUID) error {
	if s.members == nil || actorID == uuid.Nil || businessID == uuid.Nil {
		return nil
	}
	member, err := s.members.GetMembership(ctx, businessID, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrMembershipNotFound) {
			return domain.ErrForbidden
		}
		return fmt.Errorf("load connect membership: %w", err)
	}
	if member.Status == memberStatusSuspended {
		return domain.ErrForbidden
	}
	role, err := s.members.GetRole(ctx, member.RoleID)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			return domain.ErrForbidden
		}
		return fmt.Errorf("load connect role: %w", err)
	}
	for _, p := range role.Permissions {
		if p == authz.PermIntegrationsConnect {
			return nil
		}
	}
	return domain.ErrForbidden
}

// gateBusiness re-loads the target business through the soft-delete-aware
// GetByID (deleted_at IS NULL) and rejects with domain.ErrBusinessNotFound when
// the organization is soft-deleted (pending erasure), before any integration row
// or token is created. This is the single choke point every platform connect
// funnels through, so it blocks new-PII ingestion (fresh OAuth/bot tokens) into
// an organization inside its deletion grace window — a window in which the
// membership row still satisfies authz.RequireBusinessAccess. It is a no-op when
// no BusinessLookup is attached (in-process callers pass a live business) or when
// businessID is the nil UUID.
func (s *integrationService) gateBusiness(ctx context.Context, businessID uuid.UUID) error {
	if s.business == nil || businessID == uuid.Nil {
		return nil
	}
	if _, err := s.business.GetByID(ctx, businessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return domain.ErrBusinessNotFound
		}
		return fmt.Errorf("load connect business: %w", err)
	}
	return nil
}

// GetDecryptedToken returns decrypted tokens, refreshing on expiry; empty externalID falls back to first-active.
// Rows with a non-nil WrappedDEK are decrypted via the envelope path; rows with
// nil WrappedDEK fall back to the legacy Encryptor inside the envelope.
// A synchronous fail-closed audit row is emitted before the token is returned;
// if the audit INSERT fails the token is never released. reason records the
// caller's purpose for the forensic trail.
// See docs/services/integration.md.
func (s *integrationService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*TokenResponse, error) {
	var integration *domain.Integration
	var err error

	if externalID != "" {
		integration, err = s.repo.GetByBusinessPlatformExternal(ctx, businessID, platform, externalID)
		if err != nil && !errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, fmt.Errorf("get integration by external id: %w", err)
		}
	}

	if integration == nil {
		integrations, listErr := s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
		if listErr != nil {
			return nil, fmt.Errorf("list integrations: %w", listErr)
		}
		for i := range integrations {
			if integrations[i].Status == domain.IntegrationStatusActive {
				integration = &integrations[i]
				break
			}
		}
		if integration == nil {
			return nil, domain.ErrIntegrationNotFound
		}
	}

	if integration.Status != domain.IntegrationStatusActive {
		return nil, domain.ErrTokenExpired
	}

	if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
		if len(integration.EncryptedRefreshToken) == 0 || s.refresher == nil || !s.refresherSupports(integration.Platform) {
			return nil, domain.ErrTokenExpired
		}

		if s.pool != nil {
			lockErr := oauthlock.RefreshWithRetry(ctx, s.pool, integration.ID, integration.Platform, func(lockCtx context.Context, tx pgx.Tx) error {
				fresh, rerr := s.repo.GetByID(lockCtx, integration.ID)
				if rerr != nil {
					return fmt.Errorf("re-read integration after lock: %w", rerr)
				}

				if fresh.TokenExpiresAt == nil || !fresh.TokenExpiresAt.Before(time.Now()) {
					integration = fresh
					return nil
				}

				refreshPts, _, derr := s.envelope.DecryptToken(lockCtx, fresh.ID, fresh.Platform, fresh.EncryptedRefreshToken, fresh.WrappedDEK)
				if derr != nil {
					return fmt.Errorf("decrypt refresh token: %w", derr)
				}
				defer crypto.Wipe(refreshPts)

				refreshHTTPCtx, refreshHTTPCancel := context.WithTimeout(lockCtx, 5*time.Second)
				defer refreshHTTPCancel()

				newAccess, newRefresh, expiresIn, refreshErr := s.refresher.RefreshToken(refreshHTTPCtx, string(refreshPts))
				if refreshErr != nil {
					slog.ErrorContext(lockCtx, "token refresh failed",
						"integration_id", fresh.ID,
						"platform", fresh.Platform,
						"error", refreshErr,
					)
					return domain.ErrTokenExpired
				}

				refreshPlaintext := []byte(newRefresh)
				if newRefresh == "" {
					refreshPlaintext = refreshPts
				}

				var userTokenPlaintext []byte
				if len(fresh.EncryptedUserToken) > 0 {
					utPts, _, uderr := s.envelope.DecryptToken(lockCtx, fresh.ID, fresh.Platform, fresh.EncryptedUserToken, fresh.WrappedDEK)
					if uderr != nil {
						return fmt.Errorf("decrypt user token: %w", uderr)
					}
					defer crypto.Wipe(utPts)
					userTokenPlaintext = utPts
				}

				pts := [][]byte{[]byte(newAccess), refreshPlaintext, userTokenPlaintext}
				cts, wrapped, ver, fp, eerr := s.envelope.EncryptForRow(lockCtx, fresh.ID, fresh.Platform, pts)
				wipeRefreshPlaintexts(pts, newRefresh != "")
				if eerr != nil {
					return fmt.Errorf("encrypt new tokens: %w", eerr)
				}
				fresh.EncryptedAccessToken = cts[0]
				fresh.EncryptedRefreshToken = cts[1]
				fresh.EncryptedUserToken = cts[2]
				fresh.WrappedDEK = wrapped
				fresh.KeyVersion = ver
				fresh.EncryptionKeyFingerprint = fp
				expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
				fresh.TokenExpiresAt = &expiresAt

				if uerr := s.repo.Update(lockCtx, fresh); uerr != nil {
					return fmt.Errorf("persist refreshed tokens: %w", uerr)
				}
				audit.LogIntegrationTokenRotated(lockCtx, s.audit, fresh.BusinessID, fresh.ID, fresh.Platform)

				slog.InfoContext(lockCtx, "token refreshed successfully",
					"integration_id", fresh.ID,
					"platform", fresh.Platform,
					"new_expiry", expiresAt.Format(time.RFC3339),
				)
				integration = fresh
				return nil
			})
			if lockErr != nil {
				if errors.Is(lockErr, oauthlock.ErrLockExhausted) {
					return nil, fmt.Errorf("oauth refresh lock contention: %w", domain.ErrServiceUnavailable)
				}
				if errors.Is(lockErr, domain.ErrTokenExpired) {
					return nil, domain.ErrTokenExpired
				}
				return nil, lockErr
			}
		} else {
			mu := s.getRefreshMutex(integration.ID)
			mu.Lock()
			defer mu.Unlock()

			integration, err = s.repo.GetByID(ctx, integration.ID)
			if err != nil {
				return nil, fmt.Errorf("re-read integration after lock: %w", err)
			}

			if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
				refreshPts, _, derr := s.envelope.DecryptToken(ctx, integration.ID, integration.Platform, integration.EncryptedRefreshToken, integration.WrappedDEK)
				if derr != nil {
					return nil, fmt.Errorf("decrypt refresh token: %w", derr)
				}
				defer crypto.Wipe(refreshPts)

				newAccess, newRefresh, expiresIn, rfErr := s.refresher.RefreshToken(ctx, string(refreshPts))
				if rfErr != nil {
					slog.ErrorContext(ctx, "token refresh failed",
						"integration_id", integration.ID,
						"platform", integration.Platform,
						"error", rfErr,
					)
					return nil, domain.ErrTokenExpired
				}

				refreshPlaintext := []byte(newRefresh)
				if newRefresh == "" {
					refreshPlaintext = refreshPts
				}

				var userTokenPlaintext []byte
				if len(integration.EncryptedUserToken) > 0 {
					utPts, _, uderr := s.envelope.DecryptToken(ctx, integration.ID, integration.Platform, integration.EncryptedUserToken, integration.WrappedDEK)
					if uderr != nil {
						return nil, fmt.Errorf("decrypt user token: %w", uderr)
					}
					defer crypto.Wipe(utPts)
					userTokenPlaintext = utPts
				}

				pts := [][]byte{[]byte(newAccess), refreshPlaintext, userTokenPlaintext}
				cts, wrapped, ver, fp, eerr := s.envelope.EncryptForRow(ctx, integration.ID, integration.Platform, pts)
				wipeRefreshPlaintexts(pts, newRefresh != "")
				if eerr != nil {
					return nil, fmt.Errorf("encrypt new tokens: %w", eerr)
				}
				integration.EncryptedAccessToken = cts[0]
				integration.EncryptedRefreshToken = cts[1]
				integration.EncryptedUserToken = cts[2]
				integration.WrappedDEK = wrapped
				integration.KeyVersion = ver
				integration.EncryptionKeyFingerprint = fp
				expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
				integration.TokenExpiresAt = &expiresAt

				if uerr := s.repo.Update(ctx, integration); uerr != nil {
					return nil, fmt.Errorf("persist refreshed tokens: %w", uerr)
				}

				audit.LogIntegrationTokenRotated(ctx, s.audit, integration.BusinessID, integration.ID, integration.Platform)

				slog.InfoContext(ctx, "token refreshed successfully",
					"integration_id", integration.ID,
					"platform", integration.Platform,
					"new_expiry", expiresAt.Format(time.RFC3339),
				)
			}
		}
	}

	var accessToken string
	var keyVersion int16
	if len(integration.EncryptedAccessToken) > 0 {
		decrypted, kv, derr := s.envelope.DecryptToken(ctx, integration.ID, integration.Platform, integration.EncryptedAccessToken, integration.WrappedDEK)
		if derr != nil {
			return nil, fmt.Errorf("decrypt access token: %w", derr)
		}
		defer crypto.Wipe(decrypted)
		accessToken = string(decrypted)
		keyVersion = kv
	}

	var userToken string
	if len(integration.EncryptedUserToken) > 0 {
		decrypted, _, derr := s.envelope.DecryptToken(ctx, integration.ID, integration.Platform, integration.EncryptedUserToken, integration.WrappedDEK)
		if derr != nil {
			return nil, fmt.Errorf("decrypt user token: %w", derr)
		}
		defer crypto.Wipe(decrypted)
		if integration.UserTokenExpiresAt == nil || integration.UserTokenExpiresAt.After(time.Now()) {
			userToken = string(decrypted)
		}
	}

	caller := middleware.ServiceIdentityFromContext(ctx)
	if caller == "" {
		caller = callerServiceInternal
	}
	correlationID := logger.CorrelationIDFromContext(ctx)
	if err := audit.LogTokenDecryptedSync(ctx, s.audit, businessID, integration.ID, platform, caller, correlationID, reason, keyVersion); err != nil {
		return nil, fmt.Errorf("audit token_decrypted: %w", err)
	}
	metrics.IncIntegrationTokenDecrypted(platform, caller)

	return &TokenResponse{
		IntegrationID:    integration.ID,
		Platform:         integration.Platform,
		ExternalID:       integration.ExternalID,
		AccessToken:      accessToken,
		UserToken:        userToken,
		Metadata:         integration.Metadata,
		ExpiresAt:        integration.TokenExpiresAt,
		UserTokenExpires: integration.UserTokenExpiresAt,
	}, nil
}

// wipeRefreshPlaintexts zeroes the freshly allocated plaintexts handed to
// EncryptForRow that are not already covered by a deferred wipe. pts[0]
// (new access token) is always a fresh allocation. pts[1] is fresh only when
// the provider rotated the refresh token; otherwise it aliases the deferred
// refreshPts. pts[2] is nil or aliases the deferred utPts. Call only after
// EncryptForRow has returned.
func wipeRefreshPlaintexts(pts [][]byte, rotated bool) {
	crypto.Wipe(pts[0])
	if rotated {
		crypto.Wipe(pts[1])
	}
}

// ListByBusinessAndPlatform retrieves all integrations for a business filtered by platform
func (s *integrationService) ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}
	return s.repo.ListByBusinessAndPlatform(ctx, businessID, platform)
}

// MarkTokenExpired flips the stored status of the matching active integration(s)
// to token_expired so the dashboard prompts a reconnect. Invoked when an agent
// reports the typed code integration_token_invalid for a dispatch. When
// externalID identifies the failing integration the flip is scoped to it alone,
// leaving sibling channels on the same platform untouched; an empty externalID
// flips every active integration for the platform. A no-op (zero rows) is normal
// — the row may already be flipped.
func (s *integrationService) MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) error {
	if businessID == uuid.Nil {
		return fmt.Errorf("business id is required")
	}
	if platform == "" {
		return fmt.Errorf("platform is required")
	}

	n, err := s.repo.MarkTokenExpired(ctx, businessID, platform, externalID)
	if err != nil {
		return fmt.Errorf("mark token expired: %w", err)
	}

	slog.InfoContext(ctx, "integration token marked expired",
		"business_id", businessID,
		"platform", platform,
		"external_id", externalID,
		"rows_affected", n,
	)

	if n > 0 {
		audit.LogIntegrationTokenExpired(ctx, s.audit, businessID, platform, externalID, int(n))
	}
	return nil
}
