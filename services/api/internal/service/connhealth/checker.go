package connhealth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// yandexProbeTimeout caps the NATS request budget for the Yandex session
// canary (get_info runs a checkSession first). The Yandex RPA agent can take
// tens of seconds, so this is deliberately generous.
const yandexProbeTimeout = 45 * time.Second

// codeIntegrationTokenInvalid is the a2a classifier code the Yandex agent
// stamps on a passport redirect / expired session (see classifyYandexError).
// a2a exposes these codes as strings, not exported consts, so we name it here.
const codeIntegrationTokenInvalid = "integration_token_invalid"

// PlatformProbe is the connect-package slice the checker needs to evaluate the
// two API-side platforms whose tokens live on the API. *connect.ConnectHandler
// satisfies it. Kept as an interface here (consumer-side) so connhealth has no
// compile-time dependency on handler/connect (which would cycle).
type PlatformProbe interface {
	CheckTelegramHealth(ctx context.Context, externalID string) Result
	CheckVKHealth(ctx context.Context, businessID uuid.UUID, externalID string) Result
}

// Dispatcher is the NATS request/reply slice the checker needs to run the
// Yandex session canary. *platform.NATSTaskPublisher satisfies it. nil disables
// the Yandex probe (returns unknown, fail-soft).
type Dispatcher interface {
	RequestTool(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error)
}

// IntegrationStore is the repository slice the checker enumerates and writes
// through. *repository.IntegrationRepository satisfies it.
type IntegrationStore interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]interface{}) error
}

// Checker computes and persists per-integration connection health. It is the
// concrete collaborator behind IntegrationHandler.VerifyIntegrations
// (synchronous per-business CheckAll) and the shared probe body of the Yandex
// worker (CheckIntegration). All writes go through UpdateMetadata (targeted
// jsonb) and are audited via integration.metadata_updated — no new audit action
// and no schema migration.
type Checker struct {
	probe    PlatformProbe
	dispatch Dispatcher // nil = Yandex probe returns unknown
	store    IntegrationStore
	auditLog audit.Logger
	nowFn    func() time.Time
}

// NewChecker constructs a Checker. probe and store are required; dispatch may be
// nil (Yandex probe then fails soft to unknown); a nil auditLog degrades to a
// no-op logger.
func NewChecker(probe PlatformProbe, dispatch Dispatcher, store IntegrationStore, auditLog audit.Logger) *Checker {
	if auditLog == nil {
		auditLog = audit.Nop()
	}
	return &Checker{
		probe:    probe,
		dispatch: dispatch,
		store:    store,
		auditLog: auditLog,
		nowFn:    time.Now,
	}
}

// CheckAll evaluates every integration of a business, persists the fail-soft
// verdict for each, and returns the per-integration results for the caller
// (verify endpoint) to surface. A per-integration probe/persist error is logged
// and skipped — one flaky channel never fails the whole verify pass.
func (c *Checker) CheckAll(ctx context.Context, businessID uuid.UUID) ([]IntegrationHealth, error) {
	integrations, err := c.store.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("connhealth: list integrations: %w", err)
	}

	out := make([]IntegrationHealth, 0, len(integrations))
	for i := range integrations {
		integ := integrations[i]
		res, _, persistErr := c.CheckIntegration(ctx, integ)
		if persistErr != nil {
			slog.WarnContext(ctx, "connhealth: integration check failed",
				"business_id", businessID, "platform", integ.Platform, "error", persistErr)
			continue
		}
		out = append(out, IntegrationHealth{
			Platform:   integ.Platform,
			ExternalID: integ.ExternalID,
			Status:     res.Status,
			ReasonCode: res.ReasonCode,
			CheckedAt:  res.CheckedAt,
		})
	}
	return out, nil
}

// CheckIntegration probes one integration, applies the fail-soft demotion rule
// against its prior stored verdict, persists the merged metadata, audits the
// write, and returns the effective (post-demotion) Result together with the
// exact metadata map it wrote. Callers that layer a further metadata write on
// top (the worker's nudge stamp / clear) MUST base that write on the returned
// map, not on the pre-check integ.Metadata: the repository UpdateMetadata does a
// whole-column replace, so a second write built from the stale pre-check copy
// would clobber the fresh verdict this call just persisted. Shared by CheckAll
// and the worker.
func (c *Checker) CheckIntegration(ctx context.Context, integ domain.Integration) (Result, map[string]interface{}, error) {
	prev := ReadFromMetadata(integ.Metadata)
	probed := c.probePlatform(ctx, integ)
	effective := DemoteOnlyIfConclusive(prev, probed)

	merged := MergeIntoMetadata(integ.Metadata, effective)
	if err := c.store.UpdateMetadata(ctx, integ.ID, merged); err != nil {
		return Result{}, nil, fmt.Errorf("update metadata: %w", err)
	}
	audit.LogIntegrationMetadataUpdated(ctx, c.auditLog, integ.BusinessID, integ.ID, integ.Platform, []string{MetadataKey})
	return effective, merged, nil
}

// probePlatform routes an integration to its platform checker. An unrecognized
// or unwired platform yields unknown (fail soft) so a new platform never causes
// a false broken before its checker is added.
func (c *Checker) probePlatform(ctx context.Context, integ domain.Integration) Result {
	now := c.nowFn().UTC()
	switch integ.Platform {
	case a2a.AgentTelegram:
		if c.probe == nil {
			return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
		}
		return c.probe.CheckTelegramHealth(ctx, integ.ExternalID)
	case a2a.AgentVK:
		if c.probe == nil {
			return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
		}
		return c.probe.CheckVKHealth(ctx, integ.BusinessID, integ.ExternalID)
	case a2a.AgentYandexBusiness:
		return c.probeYandex(ctx, integ, now)
	default:
		return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
	}
}

// probeYandex dispatches the get_info session canary to the Yandex RPA agent.
// Success => active. A coded integration_token_invalid (passport redirect /
// session expired) => broken/session-expired. rate_limit_exceeded (captcha) and
// any NATS transport/timeout => unknown (fail soft). This never flips
// Integration.Status — the agent's own error path already flips token_expired
// on rejection; health is additive.
func (c *Checker) probeYandex(ctx context.Context, integ domain.Integration, now time.Time) Result {
	if c.dispatch == nil {
		return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
	}
	req := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       tools.YandexBusinessGetInfo,
		Args:       map[string]interface{}{},
		BusinessID: integ.BusinessID.String(),
	}
	resp, err := c.dispatch.RequestTool(ctx, a2a.Subject(a2a.AgentYandexBusiness), req, yandexProbeTimeout)
	if err != nil {
		if a2a.CodeOf(err) == codeIntegrationTokenInvalid {
			return Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: now}
		}
		return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
	}
	if resp == nil || !resp.Success {
		code := ""
		if resp != nil {
			code = resp.Code
		}
		if code == codeIntegrationTokenInvalid {
			return Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: now}
		}
		return Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: now}
	}
	return Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: now}
}
