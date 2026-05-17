package chatproxy

import (
	"context"
	"log/slog"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
)

// MessagePersister centralizes the chat_proxy persistence sequence:
//   - PersistUserMessage at request open
//   - PersistAssistantPause when SSE emits tool_approval_required
//   - PersistAssistantComplete on done / error
//   - FireAutoTitleIfPending / FireAutoTitleIfPendingResume after persist
//
// The auto-title hooks re-read the conversation AFTER persist (Landmine 4 /
// Pitfall 7) and only fire when title_status is still "auto_pending"; manual
// or auto are terminal. Spawn ctx is detached 30s — r.Context() is canceled
// at SSE close and the cheap-LLM call takes 3-8s.
type MessagePersister struct {
	msgs   domain.MessageRepository
	convs  domain.ConversationRepository
	titler TitlerService // optional; nil → graceful disable
}

// NewMessagePersister constructs a MessagePersister. msgs and convs are
// required; titler may be nil (graceful disable when titling is off).
func NewMessagePersister(msgs domain.MessageRepository, convs domain.ConversationRepository, titler TitlerService) *MessagePersister {
	if msgs == nil {
		panic("chatproxy.NewMessagePersister: msgs cannot be nil")
	}
	if convs == nil {
		panic("chatproxy.NewMessagePersister: convs cannot be nil")
	}
	return &MessagePersister{
		msgs:   msgs,
		convs:  convs,
		titler: titler,
	}
}

// PersistUserMessage writes the user message via Create. Errors are returned
// for the caller to log; the chat flow continues regardless (legacy behavior
// per chat_proxy.go:306-308).
func (p *MessagePersister) PersistUserMessage(ctx context.Context, msg *domain.Message) error {
	return p.msgs.Create(ctx, msg)
}

// PersistAssistantPause writes the assistant Message at the
// tool_approval_required pause point. Status=pending_approval; ToolCalls
// already carry ApprovalID=<batch>-<call> + Status=pending. Mirrors the
// chat_proxy.go pause sequence verbatim.
func (p *MessagePersister) PersistAssistantPause(ctx context.Context, msg *domain.Message) error {
	return p.msgs.Create(ctx, msg)
}

// PersistAssistantComplete writes the assistant Message at done/error. The
// caller has already merged streamErrContent into msg.Content. Errors are
// logged at the caller; we return them so the caller can decide.
func (p *MessagePersister) PersistAssistantComplete(ctx context.Context, msg *domain.Message) error {
	return p.msgs.Create(ctx, msg)
}

// PersistAssistantUpdate writes the assistant Message via Update — used by
// the resume path's done/error branches where the same Message ID must be
// preserved.
func (p *MessagePersister) PersistAssistantUpdate(ctx context.Context, msg *domain.Message) error {
	return p.msgs.Update(ctx, msg)
}

// FireAutoTitleIfPending re-reads the conversation AFTER messageRepo.Create
// returned and spawns the titler goroutine when title_status is still
// "auto_pending". Pitfalls 2 + 7 / Landmines 4 + 5.
//
// The re-read is mandatory: a manual rename arriving between the request
// entering chat_proxy and reaching this fire-point would leave a stale
// pre-persist snapshot showing auto_pending and clobber the rename. The
// re-read closes that window — the atomic UpdateTitleIfPending in the titler
// is a second line of defense (Landmine 8).
//
// Spawn ctx is detached 30s — r.Context() is canceled at SSE close and the
// cheap-LLM call takes 3-8s. The 5s persistCtx timeout used elsewhere in this
// file is too tight for the LLM call (Landmine 5 / Pitfall 2).
func (p *MessagePersister) FireAutoTitleIfPending(persistCtx PersistContextFn, conversationID, businessID, userText, assistantText string) {
	if p.titler == nil {
		return // graceful no-op when titling is disabled
	}

	// Re-read the conversation AFTER persist (Landmine 4 / Pitfall 7).
	ctx, cancel := persistCtx()
	defer cancel()
	conv, err := p.convs.GetByID(ctx, conversationID)
	if err != nil {
		slog.WarnContext(ctx, "auto-title gate: conversation lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return // only fires on auto_pending; manual + auto are terminal.
	}

	// Detached 30s ctx for the titler goroutine. The cancel is wired through
	// a small watcher so we never leak the timer goroutine if titler exits
	// early; vet would also flag a discarded cancel. Locale is copied off the
	// persist ctx (which inherited it from the request's middleware.Locale
	// chain) so the cheap-LLM call sees the correct language tag — Phase D2.
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnCtx = i18n.WithLocale(spawnCtx, i18n.LocaleFromContext(ctx))
	go p.titler.GenerateAndSave(spawnCtx, businessID, conversationID, userText, assistantText)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}

// FireAutoTitleIfPendingResume is the resume-path counterpart of
// FireAutoTitleIfPending. It applies the same gate but pulls businessID and
// the most recent user message from history because req.Message is not in
// scope at the streamResume "done" branch (the resume request body is empty).
//
// Same Landmine 4 / 5 disciplines apply: GetByID after persist, detached 30s
// spawn ctx, nil-titler graceful disable.
func (p *MessagePersister) FireAutoTitleIfPendingResume(persistCtx PersistContextFn, conversationID string, assistantMsg *domain.Message) {
	if p.titler == nil {
		return // graceful no-op when titling is disabled
	}
	ctx, cancel := persistCtx()
	defer cancel()
	conv, err := p.convs.GetByID(ctx, conversationID)
	if err != nil {
		slog.WarnContext(ctx, "resume auto-title gate: conv lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return
	}
	msgs, err := p.msgs.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.WarnContext(ctx, "resume auto-title gate: list messages failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	var userText string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userText = msgs[i].Content
			break
		}
	}
	// Locale copied off the persist ctx so the resume-path titler runs in the
	// same language as the original chat. Phase D2.
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	spawnCtx = i18n.WithLocale(spawnCtx, i18n.LocaleFromContext(ctx))
	go p.titler.GenerateAndSave(spawnCtx, conv.BusinessID, conversationID, userText, assistantMsg.Content)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}
