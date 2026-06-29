package chatturn

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/logger"
)

// persistContext returns the standard 5-second detached context used for
// post-stream persistence ops. The original request ctx is canceled when the
// SSE response closes, so we MUST run persist ops on a fresh ctx — otherwise
// the user navigating away mid-stream silently drops the assistant message.
//
// correlation_id and locale are propagated so logs + the auto-titler spawn
// see the same identifiers as the request edge.
func (t *Turn) persistContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ctx = i18n.WithLocale(ctx, i18n.LocaleFromContext(parentCtx))
	if corrID := logger.CorrelationIDFromContext(parentCtx); corrID != "" {
		ctx = logger.WithCorrelationID(ctx, corrID)
	}
	return ctx, cancel
}

// persistUserMessage writes the user message via MessageRepository.Create.
// Errors are logged at the caller; the chat flow continues regardless —
// failing to persist the user's question is a non-fatal observability gap,
// not a request failure.
func (t *Turn) persistUserMessage(ctx context.Context, msg *domain.Message) error {
	err := t.deps.Messages.Create(ctx, msg)
	t.bumpLastMessageAt(ctx, msg)
	return err
}

// persistAssistantPlaceholder writes the in_progress assistant Message reserved
// before the stream opens. It serializes concurrent fresh turns: a second
// POST /chat/{id} for the same conversation finds this active message via
// FindByConversationActive and gates on the in-flight turn instead of running a
// second parallel fresh turn. The finalize paths (persistAssistantComplete /
// persistAssistantPause) reuse this same _id so the turn never strands; a crash
// before finalize is reclaimed by the gateHealStranded heal path.
func (t *Turn) persistAssistantPlaceholder(ctx context.Context, msg *domain.Message) error {
	err := t.deps.Messages.Create(ctx, msg)
	t.bumpLastMessageAt(ctx, msg)
	return err
}

// finalizeAssistantMessage upserts the assistant Message by _id. The reservation
// (persistAssistantPlaceholder) normally inserted the row already, so the finalize
// is an Update. But the reservation is best-effort — if its Create failed (Mongo
// blip / timeout / dup-key) the row is ABSENT and Update returns ErrMessageNotFound
// with MatchedCount==0, which would otherwise SILENTLY DROP the assistant message.
// On that not-found we fall back to Create so the message is still persisted,
// preserving the pre-reservation behavior (always insert when no placeholder
// exists). A genuine transient Update error (not not-found) is returned unchanged.
func (t *Turn) finalizeAssistantMessage(ctx context.Context, msg *domain.Message) error {
	err := t.deps.Messages.Update(ctx, msg)
	if errors.Is(err, domain.ErrMessageNotFound) {
		slog.WarnContext(ctx, "chatturn: assistant placeholder absent at finalize, inserting (reservation likely failed)",
			"message_id", msg.ID, "conversation_id", msg.ConversationID)
		err = t.deps.Messages.Create(ctx, msg)
	}
	t.bumpLastMessageAt(ctx, msg)
	return err
}

// persistAssistantPause finalizes the reserved assistant Message at the
// tool_approval_required pause point. Status=PendingApproval; ToolCalls
// already carry ApprovalID=<batch>-<call> + Status=Pending. Upserts by _id: the
// reservation already inserted the row, but a failed reservation falls back to
// Create so the message is never dropped.
func (t *Turn) persistAssistantPause(ctx context.Context, msg *domain.Message) error {
	return t.finalizeAssistantMessage(ctx, msg)
}

// persistAssistantComplete finalizes the reserved assistant Message at done /
// error. The caller has already merged streamErrContent into msg.Content via the
// i18n wrapper so chat history renders in the writer's language forever. Upserts
// by _id: the reservation already inserted the row, but a failed reservation
// falls back to Create so the message is never dropped.
func (t *Turn) persistAssistantComplete(ctx context.Context, msg *domain.Message) error {
	return t.finalizeAssistantMessage(ctx, msg)
}

// bumpLastMessageAt advances the conversation's recency sort key to the just-
// appended message's timestamp. Best-effort: a failure is a non-fatal recency
// drift (the next append retries), so it is logged, never surfaced to the
// caller. Skips work when the message carries no usable timestamp.
func (t *Turn) bumpLastMessageAt(ctx context.Context, msg *domain.Message) {
	if t.deps.Conversations == nil || msg == nil || msg.ConversationID == "" {
		return
	}
	ts := msg.CreatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	if err := t.deps.Conversations.BumpLastMessageAt(ctx, msg.ConversationID, ts); err != nil {
		slog.WarnContext(ctx, "chatturn: failed to bump last_message_at",
			"error", err, "conversation_id", msg.ConversationID)
	}
}

// bumpLastMessageAtNow advances the conversation's recency sort key to now. The
// resume write-back paths mutate a message that was created on an earlier turn,
// so they bump with the resume timestamp (not the stale message CreatedAt).
// Best-effort: a failure is a non-fatal recency drift, logged not surfaced.
func (t *Turn) bumpLastMessageAtNow(ctx context.Context, conversationID string) {
	if t.deps.Conversations == nil || conversationID == "" {
		return
	}
	if err := t.deps.Conversations.BumpLastMessageAt(ctx, conversationID, time.Now()); err != nil {
		slog.WarnContext(ctx, "chatturn: failed to bump last_message_at",
			"error", err, "conversation_id", conversationID)
	}
}

// fireAutoTitleIfPending re-reads the conversation AFTER messageRepo.Create
// returned and spawns the titler goroutine when title_status is still
// auto_pending.
//
// LANDMINES preserved verbatim from chatproxy.MessagePersister:
//
//   - Re-read is MANDATORY (landmine 4 / pitfall 7). A manual rename
//     arriving between the request entering chatturn and reaching this
//     fire-point would leave a stale pre-persist snapshot showing
//     auto_pending and clobber the rename. The re-read closes that window;
//     the atomic UpdateTitleIfPending in the titler is a second line of
//     defense (landmine 8).
//   - Spawn ctx is detached 30s (landmine 5 / pitfall 2). The 5s
//     persistCtx used elsewhere is too tight for the cheap-LLM call (3-8s).
//   - Locale is copied off the persist ctx so the titler runs in the same
//     language as the chat.
//   - cancel is wired through a watcher goroutine so vet doesn't flag a
//     discarded cancel and the timer goroutine cannot leak.
//
// FireAutoTitleIfPending is the public entry point for legacy handler-side
// tests that exercise the gate predicate directly. Production callers should
// not need this — Turn.Run calls fireAutoTitleIfPending internally.
func (t *Turn) FireAutoTitleIfPending(parentCtx context.Context, conversationID, businessID, userText, assistantText string) {
	t.fireAutoTitleIfPending(parentCtx, conversationID, businessID, userText, assistantText)
}

// FireAutoTitleIfPendingResume mirrors FireAutoTitleIfPending for the resume
// path.
func (t *Turn) FireAutoTitleIfPendingResume(parentCtx context.Context, conversationID string, assistantMsg *domain.Message) {
	t.fireAutoTitleIfPendingResume(parentCtx, conversationID, assistantMsg)
}

func (t *Turn) fireAutoTitleIfPending(parentCtx context.Context, conversationID, businessID, userText, assistantText string) {
	if t.deps.Titler == nil {
		return
	}

	gateCtx, cancel := t.persistContext(parentCtx)
	defer cancel()

	conv, err := t.deps.Conversations.GetByID(gateCtx, conversationID)
	if err != nil {
		slog.WarnContext(gateCtx, "auto-title gate: conversation lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return
	}

	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnCtx = i18n.WithLocale(spawnCtx, i18n.LocaleFromContext(gateCtx))
	if corrID := logger.CorrelationIDFromContext(gateCtx); corrID != "" {
		spawnCtx = logger.WithCorrelationID(spawnCtx, corrID)
	}
	go t.deps.Titler.GenerateAndSave(spawnCtx, businessID, conversationID, userText, assistantText)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}

// fireAutoTitleIfPendingResume is the resume-path counterpart. It pulls
// businessID and the most recent user message from history because the
// resume request body is empty (the original message is already persisted).
//
// Same landmine 4 / 5 disciplines apply: GetByID after persist, detached 30s
// spawn ctx, nil-titler graceful disable.
func (t *Turn) fireAutoTitleIfPendingResume(parentCtx context.Context, conversationID string, assistantMsg *domain.Message) {
	if t.deps.Titler == nil {
		return
	}
	gateCtx, cancel := t.persistContext(parentCtx)
	defer cancel()

	conv, err := t.deps.Conversations.GetByID(gateCtx, conversationID)
	if err != nil {
		slog.WarnContext(gateCtx, "resume auto-title gate: conv lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return
	}
	msgs, err := t.deps.Messages.ListByConversationID(gateCtx, conversationID, t.deps.HistoryLimit, 0)
	if err != nil {
		slog.WarnContext(gateCtx, "resume auto-title gate: list messages failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	var userText string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == domain.MessageRoleUser {
			userText = msgs[i].Content
			break
		}
	}
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnCtx = i18n.WithLocale(spawnCtx, i18n.LocaleFromContext(gateCtx))
	if corrID := logger.CorrelationIDFromContext(gateCtx); corrID != "" {
		spawnCtx = logger.WithCorrelationID(spawnCtx, corrID)
	}
	go t.deps.Titler.GenerateAndSave(spawnCtx, conv.BusinessID, conversationID, userText, assistantMsg.Content)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}
