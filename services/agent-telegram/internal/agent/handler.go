package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/telegramcallback"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

// TokenInfo aliases agentbase.TokenInfo so test files that construct
// agent.TokenInfo{...} continue to compile after the agentbase migration.
// New callers should use agentbase.TokenInfo directly.
type TokenInfo = agentbase.TokenInfo

// TokenFetcher aliases agentbase.TokenResolver — same interface contract,
// kept as a type alias so test mocks declared against agent.TokenFetcher
// remain byte-identical (import-path-only test changes).
type TokenFetcher = agentbase.TokenResolver

// Sender abstracts Telegram message sending for testability. The chat target is
// a string because Telegram's chat_id accepts either a numeric ID or a public
// @channelusername — bot.go picks the right tgbotapi constructor per form.
// The channel-post sends return the delivery receipt (message id + public
// username when the channel has one) so the tool result can carry a permalink.
type Sender interface {
	SendMessage(chat, text string) (telegram.SentMessage, error)
	SendPhoto(chat, photoURL, caption string) (telegram.SentMessage, error)
	SendReply(chat string, messageID int, text string) error
	GetReviews(limit int) ([]map[string]interface{}, error)
	// SendMessageWithMarkup is the additive send path that carries an optional
	// inline keyboard. A nil markup is exactly equivalent to SendMessage (minus
	// the receipt, which DM notifications never need), so existing callers are
	// unaffected and the [Approve]/[Reject] approval buttons are strictly opt-in.
	SendMessageWithMarkup(chat, text string, markup *tgbotapi.InlineKeyboardMarkup) error
}

// SenderFactory creates a Sender from a bot token.
type SenderFactory func(botToken string) (Sender, error)

// resolveChatTarget chooses the Telegram chat_id for a send. The one OneVoice
// system bot administers every tenant's connected channel, so an unverified
// LLM-supplied channel_id must never be trusted as the target: it could name a
// DIFFERENT tenant's channel (via hallucination or prompt injection through
// untrusted review/comment text) and the shared bot — being an admin there —
// would post the acting tenant's content onto the victim's channel. resolved is
// always one of the ACTING business's OWN integration external_ids: it equals
// supplied exactly when supplied matched one of that business's integrations,
// and is the business's first-active own channel otherwise (the token resolver's
// fallback). We therefore honor supplied only when it exactly equals resolved (a
// legitimate own-channel target selected by external_id); every other supplied
// value falls back to the owned resolved channel, making it impossible to post
// to a channel the acting business does not own. Telegram accepts either a
// numeric ID or a public @channelusername, both stored by the connect flow.
// Errors with channel_not_found only when the owned channel is itself unusable.
// See AGENTS.md §"Channel ID Resolution Pattern".
func resolveChatTarget(supplied, resolved string) (string, error) {
	if supplied != "" && supplied == resolved && isValidChatTarget(supplied) {
		return supplied, nil
	}
	if isValidChatTarget(resolved) {
		return resolved, nil
	}
	return "", a2a.NewCodedError("channel_not_found",
		fmt.Errorf("telegram: invalid chat target (channel_id=%q, resolved=%q)", supplied, resolved))
}

// isValidChatTarget reports whether s is a usable Telegram chat_id: a numeric ID
// or a public @channelusername. Anything else (empty string, business name, etc.)
// is rejected so the caller falls back to the integration's external_id.
func isValidChatTarget(s string) bool {
	if strings.HasPrefix(s, "@") {
		return len(s) > 1
	}
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

// Handler is the Telegram agent's per-request processor. Its Handle method
// satisfies a2a.Exec and is wired into a2a.NewAgent from cmd/main.go. The
// dispatch chain (dispatcher fallback + per-tool routing + "unknown tool"
// error) lives in agentbase.NewRouter; this struct only owns the per-tool
// methods + their dependencies.
type Handler struct {
	tokens        TokenFetcher
	senderFactory SenderFactory
	exec          agentbase.ToolExec
	// approvalHMACSecret signs the callback_data on the [Approve]/[Reject]
	// buttons attached to an owner approval notification. Empty disables the
	// buttons fail-closed — the notification still sends, just without the inline
	// keyboard, so no unsigned approval surface is ever exposed.
	approvalHMACSecret string
}

// Option configures a Handler at construction time.
type Option func(*Handler)

// WithApprovalHMACSecret enables the inline approve/reject buttons on owner
// notifications by supplying the HMAC secret used to sign each button's opaque
// callback_data. An empty secret is a no-op (buttons stay disabled fail-closed).
func WithApprovalHMACSecret(secret string) Option {
	return func(h *Handler) { h.approvalHMACSecret = secret }
}

// NewHandler creates a Handler with the given TokenFetcher, SenderFactory, and
// agentbase.Dispatcher. The dispatcher owns the HITL dedupe gate and error
// classification — see pkg/agentbase. Tests may pass a dispatcher built with
// nil dedupe / nil classifier, or pass nil here to skip HITL entirely. On the
// nil-dispatcher path the router applies ClassifyTelegramError as the fallback
// classifier so the contract matches the legacy Handle implementation. Optional
// behavior (e.g. approval buttons) is supplied via Option.
func NewHandler(tokens TokenFetcher, factory SenderFactory, dispatcher agentbase.Dispatcher, opts ...Option) *Handler {
	h := &Handler{tokens: tokens, senderFactory: factory}
	for _, opt := range opts {
		opt(h)
	}
	h.exec = agentbase.NewRouter(h.routes(), dispatcher, agentbase.FuncClassifier(ClassifyTelegramError))
	return h
}

// Handle is the a2a.Exec entry point — a thin shim over the router built in
// NewHandler. Kept as a method on Handler so the existing test surface
// (h.Handle(ctx, req)) continues to work byte-identically.
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return h.exec(ctx, req)
}

// routes binds the Telegram tool catalog to the Handler's per-tool methods.
// Called once from NewHandler to seed the agentbase.Router.
func (h *Handler) routes() map[string]agentbase.ToolExec {
	return map[string]agentbase.ToolExec{
		tools.TelegramSendChannelPost:  h.sendChannelPost,
		tools.TelegramSendChannelPhoto: h.sendChannelPhoto,
		tools.TelegramSendNotification: h.sendNotification,
		tools.TelegramGetReviews:       h.getReviews,
		tools.TelegramReplyToComment:   h.replyToComment,
	}
}

// ClassifyTelegramError wraps permanent Telegram API errors as NonRetryableError.
// Checks error message strings since tgbotapi returns errors with descriptions.
// Exported so cmd/main.go can wire it through agentbase.FuncClassifier; the
// dispatcher invokes it after exec returns.
func ClassifyTelegramError(err error) error {
	return classifyTelegramError(err)
}

// classifyTelegramError is the internal implementation used by per-tool
// handlers (sendChannelPost wraps with %w). It stamps a typed Code on every
// non-nil error so the frontend can render a localized explanation from
// the locked enum: integration_token_invalid, rate_limit_exceeded,
// media_too_large, channel_not_found, transient.
func classifyTelegramError(err error) error {
	if err == nil {
		return nil
	}
	if a2a.CodeOf(err) != "" {
		return err
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(msg, "Unauthorized") || strings.Contains(msg, "Forbidden") {
		return a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "Too Many Requests") || strings.Contains(msg, "retry after") {
		return a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(fmt.Errorf("telegram rate limit: %w", err)))
	}
	if strings.Contains(lower, "photo_invalid_dimensions") || strings.Contains(lower, "photo dimensions") ||
		strings.Contains(lower, "file too big") || strings.Contains(lower, "photo_save_file_invalid") {
		return a2a.NewCodedError("media_too_large", a2a.NewNonRetryableError(err))
	}
	if strings.Contains(msg, "chat not found") || strings.Contains(msg, "Bad Request: chat_id is empty") {
		return a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(err))
	}
	return a2a.NewCodedError("transient", err)
}

// getSender retrieves a Sender and the resolved externalID for a tool request.
// When externalID is empty, the first active integration for the business is used.
func (h *Handler) getSender(ctx context.Context, req a2a.ToolRequest, externalID string) (Sender, string, error) {
	info, err := agentbase.FetchToken(ctx, h.tokens, req.BusinessID, a2a.AgentTelegram, externalID, req.Tool)
	if err != nil {
		return nil, "", err
	}
	sender, err := h.senderFactory(info.AccessToken)
	if err != nil {
		return nil, "", a2a.NewNonRetryableError(fmt.Errorf("create sender: %w", err))
	}
	return sender, info.ExternalID, nil
}

// telegramPermalinkBase is the public t.me prefix a channel-post permalink is
// built from: https://t.me/<username>/<message_id>.
const telegramPermalinkBase = "https://t.me/"

// channelPostURL builds the public permalink of a delivered channel post.
// Telegram only exposes permalinks for channels with a public @username: the
// send receipt carries it regardless of how the send was addressed, and the
// @username target itself is the fallback for responses that omit the chat.
// Private channels have neither — the permalink is then empty and the tool
// result simply carries no url.
func channelPostURL(sent telegram.SentMessage, target string) string {
	username := sent.ChatUsername
	if username == "" && strings.HasPrefix(target, "@") {
		username = target[1:]
	}
	if username == "" || sent.MessageID <= 0 {
		return ""
	}
	return telegramPermalinkBase + username + "/" + strconv.Itoa(sent.MessageID)
}

// sentPostResult builds the tool-result payload of a delivered channel post:
// the message id always, the t.me permalink when the channel is public.
func sentPostResult(sent telegram.SentMessage, target string) map[string]any {
	result := map[string]any{"status": "sent", "message_id": sent.MessageID}
	if url := channelPostURL(sent, target); url != "" {
		result["url"] = url
	}
	return result
}

func (h *Handler) sendChannelPost(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}

	target, err := resolveChatTarget(channelIDStr, resolvedID)
	if err != nil {
		return nil, err
	}

	sent, err := sender.SendMessage(target, text)
	if err != nil {
		return nil, fmt.Errorf("telegram: send message: %w", classifyTelegramError(err))
	}

	return a2a.OK(req, sentPostResult(sent, target)), nil
}

func (h *Handler) sendChannelPhoto(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	photoURL, _ := req.Args["photo_url"].(string)
	caption, _ := req.Args["caption"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}

	target, err := resolveChatTarget(channelIDStr, resolvedID)
	if err != nil {
		return nil, err
	}

	sent, err := sender.SendPhoto(target, photoURL, caption)
	if err != nil {
		return nil, fmt.Errorf("telegram: send photo: %w", classifyTelegramError(err))
	}

	return a2a.OK(req, sentPostResult(sent, target)), nil
}

// sendNotification delivers a private message to the business owner. Unlike the
// channel-post tools it must NEVER fall back to the integration's external_id —
// that is the connected public channel, and broadcasting an owner-private
// notification (which may quote third-party customer PII) there would be a
// confidentiality leak. The target is therefore resolved only from an
// explicitly supplied private chat_id; absent a valid private recipient the
// path fails closed with notification_recipient_unconfigured rather than
// reusing the channel id as a DM target.
func (h *Handler) sendNotification(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)

	if !isValidChatTarget(chatIDStr) {
		return nil, a2a.NewCodedError("notification_recipient_unconfigured",
			a2a.NewNonRetryableError(fmt.Errorf("telegram: no private owner recipient configured for notification")))
	}

	sender, _, err := h.getSender(ctx, req, chatIDStr)
	if err != nil {
		return nil, err
	}

	markup, err := h.approvalMarkup(req.Args)
	if err != nil {
		return nil, err
	}

	if err := sender.SendMessageWithMarkup(chatIDStr, text, markup); err != nil {
		return nil, fmt.Errorf("telegram: send notification: %w", classifyTelegramError(err))
	}

	return a2a.OK(req, map[string]any{"status": "sent"}), nil
}

// approvalMarkup builds the optional [Approve]/[Reject] inline keyboard for an
// owner approval notification. It returns nil (plain send) when no
// approval_batch_id arg is present or the HMAC secret is unset — the buttons are
// strictly opt-in and fail closed to a plain notification when signing is
// unavailable, never to an unsigned button. locale (ru|en) selects the
// owner-facing labels; anything else defaults to ru. A build error on a present
// batch id is surfaced (non-retryable) rather than silently dropping the buttons,
// so a misconfiguration is visible instead of degrading to an approval-less DM.
func (h *Handler) approvalMarkup(args map[string]interface{}) (*tgbotapi.InlineKeyboardMarkup, error) {
	batchID, _ := args["approval_batch_id"].(string)
	if batchID == "" {
		return nil, nil
	}
	if h.approvalHMACSecret == "" {
		slog.Warn("telegram agent: approval_batch_id supplied but TELEGRAM_APPROVAL_HMAC_SECRET unset — sending notification without inline buttons")
		return nil, nil
	}
	approveData, err := telegramcallback.BuildCallbackData(batchID, telegramcallback.ActionApprove, h.approvalHMACSecret)
	if err != nil {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: build approve callback: %w", err))
	}
	rejectData, err := telegramcallback.BuildCallbackData(batchID, telegramcallback.ActionReject, h.approvalHMACSecret)
	if err != nil {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: build reject callback: %w", err))
	}
	locale, _ := args["locale"].(string)
	approveLabel, rejectLabel := approvalButtonLabels(locale)
	markup := telegram.ApprovalKeyboard(approveLabel, approveData, rejectLabel, rejectData)
	return &markup, nil
}

// approvalButtonLabels returns the owner-facing approve/reject button captions
// for the given locale. ru is the default; en is selected for an "en" locale.
func approvalButtonLabels(locale string) (approve, reject string) {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return "Approve", "Reject"
	}
	return "Одобрить", "Отклонить"
}

func (h *Handler) getReviews(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	limit, err := a2a.GetIntParam(req.Args, "limit", domain.TelegramReviewLimitDefault)
	if err != nil {
		slog.Warn("telegram agent: invalid limit param, using default", "error", err)
		limit = domain.TelegramReviewLimitDefault
	}
	if limit <= 0 {
		limit = domain.TelegramReviewLimitDefault
	}
	if limit > domain.TelegramReviewLimitMax {
		limit = domain.TelegramReviewLimitMax
	}

	sender, _, err := h.getSender(ctx, req, "")
	if err != nil {
		return nil, err
	}

	reviews, err := sender.GetReviews(limit)
	if err != nil {
		return nil, fmt.Errorf("telegram: get reviews: %w", classifyTelegramError(err))
	}

	return a2a.OK(req, map[string]any{"reviews": reviews, "count": len(reviews)}), nil
}

func (h *Handler) replyToComment(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	text, _ := req.Args["text"].(string)
	chatIDStr, _ := req.Args["chat_id"].(string)
	channelIDStr, _ := req.Args["channel_id"].(string)

	var messageID int
	switch v := req.Args["message_id"].(type) {
	case float64:
		messageID = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: invalid message_id %q: %w", v, err))
		}
		messageID = parsed
	}
	if messageID <= 0 {
		return nil, a2a.NewNonRetryableError(fmt.Errorf("telegram: message_id is required and must be > 0"))
	}

	slog.Info("telegram agent: reply_to_comment", "chat_id", chatIDStr, "message_id", messageID, "text_len", len(text))

	sender, resolvedID, err := h.getSender(ctx, req, channelIDStr)
	if err != nil {
		return nil, err
	}

	target, err := resolveChatTarget(chatIDStr, resolvedID)
	if err != nil {
		return nil, err
	}

	if err := sender.SendReply(target, messageID, text); err != nil {
		return nil, fmt.Errorf("telegram: reply to comment: %w", classifyTelegramError(err))
	}

	return a2a.OK(req, map[string]any{"status": "replied"}), nil
}
