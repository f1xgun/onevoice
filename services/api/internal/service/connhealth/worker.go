package connhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// workerConcurrency bounds how many integrations a single RunOnce pass probes at
// once, so a fleet-wide tick never floods the RPA agent with parallel sessions.
const workerConcurrency = 4

// nudgeDispatchTimeout caps the per-nudge NATS request budget.
const nudgeDispatchTimeout = 30 * time.Second

// nudgeCooldown throttles repeat owner nudges: even across a broken→active→broken
// cycle, a fresh nudge waits at least this long. The transition-only gate is the
// primary guard; the cooldown is a belt-and-suspenders floor against flapping.
const nudgeCooldown = 7 * 24 * time.Hour

// natsRequester is the request/reply slice the worker needs to dispatch the
// owner nudge. *natslib.Conn satisfies it; nil disables dispatch (Mongo-only
// mode: RunOnce still records health, never nudges).
type natsRequester interface {
	RequestMsgWithContext(ctx context.Context, msg *natslib.Msg) (*natslib.Msg, error)
}

// integrationLister is the enumeration slice the worker needs.
// *repository.IntegrationRepository satisfies it.
type integrationLister interface {
	ListAllActiveByPlatforms(ctx context.Context, platforms []string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]interface{}) error
}

// Worker is the proactive connection-health ticker. It enumerates active Yandex
// integrations, re-probes each session, persists the fail-soft verdict, and DMs
// the bound owner exactly once on a transition into broken. It is a no-op
// (RunOnce returns nil) when nc is nil, so a Mongo-only deploy simply never
// nudges.
type Worker struct {
	integRepo integrationLister
	checker   *Checker
	nc        natsRequester // nil = no dispatch (Mongo-only mode)
	now       func() time.Time
}

// NewWorker constructs a Worker. A nil nc leaves the worker recording health
// without ever dispatching a nudge. The clock defaults to time.Now.
func NewWorker(integRepo integrationLister, checker *Checker, nc natsRequester) *Worker {
	return &Worker{
		integRepo: integRepo,
		checker:   checker,
		nc:        nc,
		now:       time.Now,
	}
}

// RunOnce runs one Yandex-focused pass: enumerate active Yandex integrations,
// probe+persist each in bounded parallel, and nudge the owner on a fresh break.
// The owner's private Telegram chat is resolved from the business's active
// Telegram integration (Yandex rows never carry a telegram_user_id), so a
// business with no bound Telegram owner records health without ever nudging.
// Per-integration errors are logged and never abort the pass. Returns nil in
// Mongo-only mode (nil nc) without touching the fleet.
func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.nc == nil {
		return nil
	}

	integrations, err := w.integRepo.ListAllActiveByPlatforms(ctx,
		[]string{a2a.AgentYandexBusiness, a2a.AgentTelegram})
	if err != nil {
		return fmt.Errorf("connhealth worker: list integrations: %w", err)
	}

	ownerChats := ownerChatsByBusiness(integrations)

	sem := make(chan struct{}, workerConcurrency)
	var wg sync.WaitGroup
	for i := range integrations {
		integ := integrations[i]
		if integ.Platform != a2a.AgentYandexBusiness {
			continue
		}
		ownerChat := ownerChats[integ.BusinessID]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := w.processIntegration(ctx, integ, ownerChat); err != nil {
				slog.ErrorContext(ctx, "connhealth worker: integration pass failed",
					"business_id", integ.BusinessID, "platform", integ.Platform, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// processIntegration probes one integration, applies the transition-aware nudge
// gate, and stamps or clears the throttle key. The prior status is read BEFORE
// the check so a transition into broken is detectable. ownerChat is the owner's
// private Telegram chat id ("" when the business has no bound owner).
//
// The nudge/clear write layers on top of the metadata CheckIntegration just
// persisted (the returned fresh map), never the stale pre-check integ.Metadata:
// the repository UpdateMetadata replaces the whole metadata column, so basing
// the second write on the pre-check copy would overwrite the fresh verdict and
// leave the dashboard badge disagreeing with the DM (broken lost) or a
// recovered channel stuck broken forever.
func (w *Worker) processIntegration(ctx context.Context, integ domain.Integration, ownerChat string) error {
	prev := ReadFromMetadata(integ.Metadata)

	effective, fresh, err := w.checker.CheckIntegration(ctx, integ)
	if err != nil {
		return fmt.Errorf("check integration: %w", err)
	}

	switch {
	case w.crossedIntoBroken(prev.Status, effective.Status) && w.nudgeDue(integ):
		w.nudgeAndStamp(ctx, integ, fresh, ownerChat)
	case effective.Status == StatusActive && !ReadNudgedAt(fresh).IsZero():
		w.clearNudge(ctx, integ, fresh)
	}
	return nil
}

// crossedIntoBroken reports whether the status transitioned INTO broken this
// pass (was not broken before, is broken now). A channel that is already broken
// is not re-nudged — this is the per-tick-spam guard.
func (w *Worker) crossedIntoBroken(prev, next Status) bool {
	return next == StatusBroken && prev != StatusBroken
}

// nudgeDue reports whether the owner-nudge cooldown has elapsed (or no nudge was
// ever sent). Combined with the transition gate this bounds nudges to at most
// one per break per cooldown window.
func (w *Worker) nudgeDue(integ domain.Integration) bool {
	last := ReadNudgedAt(integ.Metadata)
	if last.IsZero() {
		return true
	}
	return w.now().Sub(last) >= nudgeCooldown
}

// nudgeAndStamp DMs the bound owner a localized reconnect nudge and, on
// dispatch success, stamps nudged_at so a retry or the next tick does not
// re-nudge. A business without a bound owner chat records health but is never
// nudged (no side channel to reach the owner). fresh is the metadata
// CheckIntegration just persisted (carrying the new broken verdict); the stamp
// layers on top of it so the whole-column write does not revert the verdict.
func (w *Worker) nudgeAndStamp(ctx context.Context, integ domain.Integration, fresh map[string]interface{}, chatID string) {
	if chatID == "" {
		return
	}

	now := w.now()
	if err := w.dispatchNudge(ctx, integ, chatID, now); err != nil {
		slog.WarnContext(ctx, "connhealth worker: nudge dispatch failed",
			"business_id", integ.BusinessID, "error", err)
		return
	}

	stamped := MergeNudgedAt(fresh, now)
	if err := w.integRepo.UpdateMetadata(ctx, integ.ID, stamped); err != nil {
		slog.WarnContext(ctx, "connhealth worker: nudge stamp failed",
			"business_id", integ.BusinessID, "error", err)
	}
}

// dispatchNudge sends the reconnect DM via telegram__send_notification with a
// stable approvalID (business + platform + date bucket) so a NATS retry dedupes
// at the agent and never double-DMs within a day.
func (w *Worker) dispatchNudge(ctx context.Context, integ domain.Integration, chatID string, now time.Time) error {
	text := i18n.TrTag(i18n.DefaultTag, "notify.connection.reconnect_yandex")
	args := map[string]interface{}{
		"text":    text,
		"chat_id": chatID,
	}
	approvalID := fmt.Sprintf("conn-health-nudge-%s-%s-%s",
		integ.BusinessID, integ.Platform, now.UTC().Format("2006-01-02"))
	req := a2a.ToolRequest{
		TaskID:     uuid.NewString(),
		Tool:       tools.TelegramSendNotification,
		Args:       args,
		BusinessID: integ.BusinessID.String(),
		ApprovalID: approvalID,
	}
	data, err := marshalToolRequest(req)
	if err != nil {
		return err
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, nudgeDispatchTimeout)
	defer cancel()
	if _, err := w.nc.RequestMsgWithContext(dispatchCtx, &natslib.Msg{
		Subject: a2a.Subject(a2a.AgentTelegram),
		Data:    data,
	}); err != nil {
		return fmt.Errorf("nats request: %w", err)
	}
	return nil
}

// clearNudge wipes the throttle key once a broken channel recovers to active, so
// a future break re-nudges the owner. fresh is the metadata CheckIntegration
// just persisted (carrying the new active verdict); clearing nudged_at layers on
// top of it so the whole-column write does not revert the recovered status back
// to the stale broken sub-object.
func (w *Worker) clearNudge(ctx context.Context, integ domain.Integration, fresh map[string]interface{}) {
	cleared := MergeNudgedAt(fresh, time.Time{})
	if err := w.integRepo.UpdateMetadata(ctx, integ.ID, cleared); err != nil {
		slog.WarnContext(ctx, "connhealth worker: nudge clear failed",
			"business_id", integ.BusinessID, "error", err)
	}
}

// ownerChatsByBusiness builds a businessID → owner private-chat lookup from the
// active Telegram integrations in the fleet. The nudge must reach the owner's
// private chat (telegram_user_id), never the public channel external_id, so a
// Yandex break resolves the recipient through the same bound-owner metadata the
// weekly owner-brief uses. First bound chat per business wins.
func ownerChatsByBusiness(integrations []domain.Integration) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string)
	for i := range integrations {
		integ := integrations[i]
		if integ.Platform != a2a.AgentTelegram {
			continue
		}
		if _, seen := out[integ.BusinessID]; seen {
			continue
		}
		if chatID := ownerChatIDFromMetadata(integ.Metadata); chatID != "" {
			out[integ.BusinessID] = chatID
		}
	}
	return out
}

// ownerChatIDFromMetadata extracts the owner's private Telegram numeric id from
// a Telegram integration's metadata, returning "" when absent or blank.
func ownerChatIDFromMetadata(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	id, _ := meta["telegram_user_id"].(string)
	return strings.TrimSpace(id)
}

// marshalToolRequest serializes an a2a.ToolRequest for NATS dispatch.
func marshalToolRequest(req a2a.ToolRequest) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return data, nil
}
