package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/telegramcallback"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
)

// approvalChannelTelegram labels the inbound plane an approval was resolved
// through, recorded on the audit event.
const approvalChannelTelegram = "telegram"

// approvalResumeTimeout bounds the headless resume driven off a Telegram
// callback. It is generous enough for the approved tool to dispatch through the
// orchestrator resume stream but bounded so a stuck orchestrator cannot pin the
// consumer goroutine.
const approvalResumeTimeout = 90 * time.Second

// approvalHandleTimeout bounds one end-to-end callback handle (parse → load →
// bind → resolve → resume → audit). It runs on a fresh context — the NATS
// message carries none — so a slow step cannot wedge the subscription callback.
const approvalHandleTimeout = approvalResumeTimeout + 10*time.Second

// chatResumer is the narrow slice of the chat-turn lifecycle the consumer needs
// to EXECUTE the approved tools after recording the verdicts: it drives the
// orchestrator resume stream and finalizes the assistant message. *chatturn.Turn
// satisfies it. Kept as an interface so the consumer's tests inject a fake.
type chatResumer interface {
	ResumeApproved(ctx context.Context, w http.ResponseWriter, conversationID, batchID string, body []byte) (chatturn.TurnOutcome, error)
}

// ownerTelegramResolver returns the verified owner Telegram user-id for a
// business's Telegram integration, read back from the metadata captured at
// connect. It is the ONLY authorization anchor for who may approve off-app.
type ownerTelegramResolver interface {
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
}

// TelegramApprovalConsumer validates and resolves an inline-button HITL approval
// callback published by the Telegram agent. It NEVER trusts the callback on its
// own: it re-verifies the HMAC nonce, binds the tapper's from.id to the batch
// business's verified owner id, and reuses the shared Resolve + ResumeApproved
// service logic (never the HTTP handler). See docs/services/telegram-approval.md.
type TelegramApprovalConsumer struct {
	hitl    *HITLService
	resumer chatResumer
	owners  ownerTelegramResolver
	audit   audit.Logger
	answer  callbackAnswerer
	secret  string
}

// callbackAnswerer sends the terminal answerCallbackQuery toast back to the
// tapper AFTER the consumer decides the outcome. The agent already sent the bare
// spinner-stopping ack; this optional second toast reports approved/rejected/
// expired. nil disables the toast (the resolve still happens). Implementations
// publish onto a NATS reply subject or call the Bot API — wired at construction.
type callbackAnswerer interface {
	Answer(ctx context.Context, callbackQueryID, text string) error
}

// NewTelegramApprovalConsumer constructs the consumer. secret is the HMAC key
// shared with the agent; an empty secret means the plane is disabled fail-closed
// (Subscribe refuses to subscribe), so absence never opens an unauthenticated
// approval path. answerer may be nil (no terminal toast).
func NewTelegramApprovalConsumer(hitl *HITLService, resumer chatResumer, owners ownerTelegramResolver, auditLogger audit.Logger, answerer callbackAnswerer, secret string) *TelegramApprovalConsumer {
	if hitl == nil {
		panic("NewTelegramApprovalConsumer: hitl cannot be nil")
	}
	if resumer == nil {
		panic("NewTelegramApprovalConsumer: resumer cannot be nil")
	}
	if owners == nil {
		panic("NewTelegramApprovalConsumer: owners cannot be nil")
	}
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &TelegramApprovalConsumer{
		hitl:    hitl,
		resumer: resumer,
		owners:  owners,
		audit:   auditLogger,
		answer:  answerer,
		secret:  secret,
	}
}

// Subscribe attaches the consumer to a2a.TelegramApprovalCallbackSubject on nc
// and returns an unsubscribe func. It refuses to subscribe (returns an error)
// when the HMAC secret is empty, so an unconfigured deployment never exposes an
// unvalidated approval plane. Each message is handled on a fresh, bounded
// context in the subscription callback goroutine.
func (c *TelegramApprovalConsumer) Subscribe(nc *natslib.Conn) (func(), error) {
	if nc == nil {
		return nil, errors.New("telegram approval consumer: nil NATS connection")
	}
	if c.secret == "" {
		return nil, errors.New("telegram approval consumer: TELEGRAM_APPROVAL_HMAC_SECRET unset — approval plane disabled")
	}
	sub, err := nc.Subscribe(a2a.TelegramApprovalCallbackSubject, func(msg *natslib.Msg) {
		var cb a2a.TelegramApprovalCallback
		if err := json.Unmarshal(msg.Data, &cb); err != nil {
			slog.Warn("telegram approval consumer: dropping malformed callback payload", "error", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), approvalHandleTimeout)
		defer cancel()
		if err := c.handle(ctx, cb); err != nil {
			slog.WarnContext(ctx, "telegram approval consumer: handle failed", "error", err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", a2a.TelegramApprovalCallbackSubject, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

// resolveOutcome is the terminal disposition of a callback, used only to pick the
// answerCallbackQuery toast copy. No branch on it changes durable state — the
// state transition already happened (or was declined) in handle.
type resolveOutcome int

const (
	outcomeApproved resolveOutcome = iota
	outcomeRejected
	outcomeExpired
	outcomeAlreadyResolved
	outcomeDenied
	outcomeError
)

// handle is the validated resolve+resume+audit core, unit-tested directly. It
// returns an error only for infrastructure failures worth logging; every
// security decline (bad nonce, wrong owner, expired, non-pending, double-tap) is
// a SAFE no-op that answers the tapper and returns nil — the callback must never
// leak why it was declined beyond a generic toast, and a decline is not an error.
func (c *TelegramApprovalConsumer) handle(ctx context.Context, cb a2a.TelegramApprovalCallback) error {
	batchID, action, err := telegramcallback.ParseAndVerify(cb.Data, c.secret)
	if err != nil {
		c.answerOutcome(ctx, cb.CallbackQueryID, outcomeDenied)
		return nil
	}

	batch, err := c.hitl.PendingRepo().GetByBatchID(ctx, batchID)
	if err != nil {
		if errors.Is(err, domain.ErrBatchNotFound) {
			c.answerOutcome(ctx, cb.CallbackQueryID, outcomeAlreadyResolved)
			return nil
		}
		return fmt.Errorf("load batch: %w", err)
	}
	if batch.Status == "expired" {
		c.answerOutcome(ctx, cb.CallbackQueryID, outcomeExpired)
		return nil
	}
	if batch.Status != "pending" {
		c.answerOutcome(ctx, cb.CallbackQueryID, outcomeAlreadyResolved)
		return nil
	}

	if !c.tapperIsOwner(ctx, batch.BusinessID, cb.FromID) {
		c.answerOutcome(ctx, cb.CallbackQueryID, outcomeDenied)
		return nil
	}

	decisions := allDecisions(batch.Calls, decisionAction(action))
	if _, err := c.hitl.Resolve(ctx, ResolveInput{
		ConversationID:  batch.ConversationID,
		BatchID:         batchID,
		ActorUserID:     batch.UserID,
		ActorBusinessID: batch.BusinessID,
		Decisions:       decisions,
	}); err != nil {
		switch {
		case errors.Is(err, ErrHITLBatchAlreadyResolving):
			c.answerOutcome(ctx, cb.CallbackQueryID, outcomeAlreadyResolved)
			return nil
		case errors.Is(err, ErrHITLBatchExpired):
			c.answerOutcome(ctx, cb.CallbackQueryID, outcomeExpired)
			return nil
		case errors.Is(err, ErrHITLBatchNotFound):
			c.answerOutcome(ctx, cb.CallbackQueryID, outcomeAlreadyResolved)
			return nil
		default:
			c.answerOutcome(ctx, cb.CallbackQueryID, outcomeError)
			return fmt.Errorf("resolve batch: %w", err)
		}
	}

	c.emitAudit(ctx, batch, action)

	if action == telegramcallback.ActionApprove {
		c.driveResume(ctx, batch)
		c.answerOutcome(ctx, cb.CallbackQueryID, outcomeApproved)
		return nil
	}
	c.answerOutcome(ctx, cb.CallbackQueryID, outcomeRejected)
	return nil
}

// tapperIsOwner reports whether fromID is the batch business's verified owner
// Telegram user-id. It FAILS CLOSED: any lookup error, an absent integration, or
// an unset/blank telegram_user_id returns false, so no takeover is possible when
// the binding anchor is unknown. The only accept path is an exact match against
// the stored, numeric owner id of an ACTIVE integration — a revoked/disconnected
// integration (Status != active) can never authorize its stale owner id, so a
// severed channel cannot approve on a business's behalf.
func (c *TelegramApprovalConsumer) tapperIsOwner(ctx context.Context, businessID string, fromID int64) bool {
	bizUUID, err := uuid.Parse(businessID)
	if err != nil {
		return false
	}
	integrations, err := c.owners.ListByBusinessAndPlatform(ctx, bizUUID, a2a.AgentTelegram)
	if err != nil {
		slog.WarnContext(ctx, "telegram approval consumer: failed to load integrations for owner binding", "error", err)
		return false
	}
	for i := range integrations {
		if integrations[i].Status != domain.IntegrationStatusActive {
			continue
		}
		ownerID, ok := ownerTelegramUserID(integrations[i].Metadata)
		if ok && ownerID == fromID {
			return true
		}
	}
	return false
}

// ownerTelegramUserID extracts the verified owner Telegram user-id from
// integration metadata. The connect flow stores telegram_user_id as a STRING;
// JSON round-trips through interface{} may also surface it as a float64, so both
// are accepted. Returns (0,false) when absent, blank, or unparseable.
func ownerTelegramUserID(metadata map[string]interface{}) (int64, bool) {
	if metadata == nil {
		return 0, false
	}
	raw, ok := metadata["telegram_user_id"]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	case float64:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}

// driveResume executes the approved tools by driving the shared resume core
// against a discard SSE sink (there is no client stream off-app). The approved
// publish/reply STILL dispatches through the orchestrator resume stream and the
// assistant message is STILL persisted. A resume failure is logged, not fatal:
// the verdict is already recorded (the owner's decision stands) and the resume
// self-heals on the next in-app request via gateHealStranded.
func (c *TelegramApprovalConsumer) driveResume(parentCtx context.Context, batch *domain.PendingToolCallBatch) {
	ctx, cancel := context.WithTimeout(parentCtx, approvalResumeTimeout)
	defer cancel()
	body := c.resumeBody(ctx, batch)
	if _, err := c.resumer.ResumeApproved(ctx, newDiscardSSEWriter(), batch.ConversationID, batch.ID, body); err != nil {
		slog.WarnContext(ctx, "telegram approval consumer: resume stream ended with error (verdict recorded; in-app self-heal will retry)",
			"error", err, "batch_id", batch.ID, "conversation_id", batch.ConversationID)
	}
}

// resumeBody assembles the resume request body so the post-approval LLM
// continuation keeps the business's platform tools available, mirroring the HTTP
// resume handler's body. Any lookup failure degrades the field to empty rather
// than failing the resume.
func (c *TelegramApprovalConsumer) resumeBody(ctx context.Context, batch *domain.PendingToolCallBatch) []byte {
	var bizApprovals map[string]domain.ToolFloor
	if bizUUID, err := uuid.Parse(batch.BusinessID); err == nil {
		if business, err := c.hitl.BusinessRepo().GetByID(ctx, bizUUID); err == nil && business != nil {
			bizApprovals = business.ToolApprovals()
		}
	}

	var projectOverrides map[string]domain.ToolFloor
	var whitelistMode string
	var allowedTools []string
	if batch.ProjectID != "" {
		if projUUID, err := uuid.Parse(batch.ProjectID); err == nil {
			if proj, err := c.hitl.ProjectRepo().GetByID(ctx, projUUID); err == nil && proj != nil {
				projectOverrides = proj.ApprovalOverrides
				whitelistMode = string(proj.WhitelistMode)
				allowedTools = proj.AllowedTools
			}
		}
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"business_approvals":         bizApprovals,
		"project_approval_overrides": projectOverrides,
		"active_integrations":        c.activeIntegrations(ctx, batch.BusinessID),
		"whitelist_mode":             whitelistMode,
		"allowed_tools":              allowedTools,
	})
	return raw
}

// activeIntegrations returns the distinct active platforms for the business,
// used to keep post-approval platform tools available on resume. A lookup
// failure degrades to an empty slice.
func (c *TelegramApprovalConsumer) activeIntegrations(ctx context.Context, businessID string) []string {
	bizUUID, err := uuid.Parse(businessID)
	if err != nil {
		return []string{}
	}
	active := make([]string, 0)
	for _, platform := range []string{a2a.AgentTelegram, a2a.AgentVK, a2a.AgentYandexBusiness} {
		integrations, err := c.owners.ListByBusinessAndPlatform(ctx, bizUUID, platform)
		if err != nil {
			continue
		}
		for i := range integrations {
			if integrations[i].Status == domain.IntegrationStatusActive {
				active = append(active, platform)
				break
			}
		}
	}
	return active
}

// emitAudit records the off-app approval resolution. Fire-and-forget: the
// resolve already succeeded, so a lost audit write must not undo it (a terminal
// failure increments the audit failure metric). actorUserID is the batch's owner
// user; a malformed one degrades to a nil actor rather than dropping the event.
func (c *TelegramApprovalConsumer) emitAudit(ctx context.Context, batch *domain.PendingToolCallBatch, action string) {
	bizUUID, err := uuid.Parse(batch.BusinessID)
	if err != nil {
		return
	}
	ownerUUID, err := uuid.Parse(batch.UserID)
	if err != nil {
		ownerUUID = uuid.Nil
	}
	audit.LogHITLApprovalResolved(ctx, c.audit, bizUUID, ownerUUID,
		batch.ID, batch.ConversationID, approvalChannelTelegram, action, len(batch.Calls))
}

// answerOutcome sends the terminal toast to the tapper when an answerer is
// wired. Best-effort — a failed toast never affects the resolve.
func (c *TelegramApprovalConsumer) answerOutcome(ctx context.Context, callbackQueryID string, outcome resolveOutcome) {
	if c.answer == nil || callbackQueryID == "" {
		return
	}
	if err := c.answer.Answer(ctx, callbackQueryID, outcomeToast(outcome)); err != nil {
		slog.WarnContext(ctx, "telegram approval consumer: failed to answer callback", "error", err)
	}
}

// decisionAction maps a callback action to the internal decision action string.
func decisionAction(action string) string {
	if action == telegramcallback.ActionReject {
		return actionReject
	}
	return actionApprove
}

// allDecisions builds a batch-wide verdict covering every call with the same
// action, matching the strict-shape requirement of HITLService.Resolve (one
// decision per call).
func allDecisions(calls []domain.PendingCall, action string) []DecisionInput {
	out := make([]DecisionInput, 0, len(calls))
	for _, c := range calls {
		out = append(out, DecisionInput{ID: c.CallID, Action: action})
	}
	return out
}

// outcomeToast returns the owner-facing toast copy for a terminal outcome. Copy
// is intentionally generic (no batch detail) so a declined callback leaks
// nothing. RU is the default owner-facing locale.
func outcomeToast(outcome resolveOutcome) string {
	switch outcome {
	case outcomeApproved:
		return "Одобрено"
	case outcomeRejected:
		return "Отклонено"
	case outcomeExpired:
		return "Запрос устарел"
	case outcomeAlreadyResolved:
		return "Уже обработано"
	default:
		return "Не удалось обработать запрос"
	}
}
