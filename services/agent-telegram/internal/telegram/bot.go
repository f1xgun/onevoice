package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/f1xgun/onevoice/pkg/netdial"
	"github.com/f1xgun/onevoice/pkg/safefetch"
)

// botAPITimeout bounds an entire Bot API round-trip. netdial only bounds the
// dial (10s); without an overall deadline a server that accepts the connection
// but stalls before sending response headers wedges the call forever, leaking
// the handler goroutine and socket and blocking graceful shutdown. tgbotapi v5
// builds requests with http.NewRequest (no context), so the handler ctx never
// reaches the transport — the client must carry the deadline itself. The review
// poll calls GetUpdates with Timeout:0 (short-poll); the approval-callback poll
// uses a bounded long-poll (callbackPollTimeout) held strictly BELOW this client
// timeout so the round-trip still cannot wedge past botAPITimeout.
const botAPITimeout = 30 * time.Second

// telegramAPIClient is the HTTP client backing the Bot API calls. It pins
// outbound dials to IPv4 — Yandex Cloud VMs have no IPv6 route, and
// api.telegram.org publishes AAAA records, so without this every Bot API
// request can hang on the dead v6 address until timeout.
var telegramAPIClient = newAPIClient(botAPITimeout)

// newAPIClient builds a Bot API HTTP client that forces IPv4 dials and bounds
// the whole round-trip by timeout, including the post-handshake wait for
// response headers and idle connection reuse.
func newAPIClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           netdial.TCP4DialContext,
			ResponseHeaderTimeout: timeout,
			IdleConnTimeout:       timeout,
		},
	}
}

// photoFetcher downloads images from user-provided (LLM-supplied) URLs with
// SSRF protection: the URL is validated (https-only, no internal addresses) and
// dialed through a screened client before any bytes are read, so a
// prompt-injected internal address cannot be reached. Full TLS verification
// applies. IPv4 is forced for the same no-IPv6 reason as telegramAPIClient.
var photoFetcher imageFetcher = safefetch.New(safefetch.Options{
	ForceIPv4: true,
})

// imageFetcher downloads a validated image URL and returns the bytes plus the
// response Content-Type. *safefetch.Fetcher satisfies it; tests substitute a
// stub so the SSRF guard does not block loopback test servers.
type imageFetcher interface {
	Get(ctx context.Context, rawURL string) (body []byte, contentType string, err error)
}

// Bot wraps the Telegram Bot API client.
type Bot struct {
	api   *tgbotapi.BotAPI
	token string
}

// New creates a Bot with the given token. Retries transient network errors
// (api.telegram.org drops ~20% of TLS handshakes on some networks).
func New(token string) (*Bot, error) {
	var api *tgbotapi.BotAPI
	err := retryTransient(defaultBotRetryAttempts, defaultBotRetryDelay, func() error {
		var e error
		api, e = tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, telegramAPIClient)
		return e
	})
	if err != nil {
		return nil, sanitizeTokenError(err, token)
	}
	return &Bot{api: api, token: token}, nil
}

// retryTransient invokes fn up to `attempts` times, backing off exponentially
// from `baseDelay`. Only network/TLS errors that are known to be safe to retry
// are considered transient; anything else is returned immediately.
func retryTransient(attempts int, baseDelay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil || !isTransientBotError(err) {
			return err
		}
		if i < attempts-1 {
			time.Sleep(baseDelay << i)
		}
	}
	return err
}

// isTransientBotError reports whether the error is a known-safe-to-retry
// failure: TLS handshake reset, connection reset, EOF before response, or
// timeout. These are pre-response failures — the server has not processed
// the request, so retry will not duplicate side effects.
func isTransientBotError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "EOF"),
		strings.Contains(msg, "unexpected eof"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "TLS handshake"),
		strings.Contains(msg, "no such host"):
		return true
	}
	return false
}

// sanitizeTokenError replaces the bot token in an error's message with a
// redaction marker, preventing the full credential from leaking into logs
// via Go's net/url default Error() implementation.
func sanitizeTokenError(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := err.Error()
	redacted := strings.ReplaceAll(msg, token, tokenRedactionMarker)
	if redacted == msg {
		return err
	}
	return errors.New(redacted)
}

// SendMessage sends a text message to the given chat. chat is a numeric ID
// (private chats/channels) or a public @channelusername — Telegram accepts
// either as chat_id.
func (b *Bot) SendMessage(chat, text string) error {
	msg := newTextMessage(chat, text)
	_, err := b.api.Send(msg)
	return sanitizeTokenError(err, b.token)
}

// SendMessageWithMarkup sends a text message with an optional inline keyboard.
// A nil markup makes it behave exactly like SendMessage — the additive path so
// callers that want [Approve][Reject] buttons opt in without changing the plain
// send. A non-nil pointer attaches the keyboard; nil leaves it off.
func (b *Bot) SendMessageWithMarkup(chat, text string, markup *tgbotapi.InlineKeyboardMarkup) error {
	msg := newTextMessage(chat, text)
	if markup != nil {
		msg.ReplyMarkup = *markup
	}
	_, err := b.api.Send(msg)
	return sanitizeTokenError(err, b.token)
}

// ApprovalKeyboard builds a two-button inline keyboard whose buttons carry the
// pre-signed opaque callback_data for the approve and reject actions. The
// caller (the send path) computes the callback_data via pkg/telegramcallback so
// bot.go stays free of the HMAC secret. Labels are the localized owner-facing
// button captions.
func ApprovalKeyboard(approveLabel, approveData, rejectLabel, rejectData string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(approveLabel, approveData),
			tgbotapi.NewInlineKeyboardButtonData(rejectLabel, rejectData),
		),
	)
}

// newTextMessage builds a MessageConfig addressed to a numeric chat ID or a
// public @channelusername, matching Telegram's dual chat_id form.
func newTextMessage(chat, text string) tgbotapi.MessageConfig {
	if id, err := strconv.ParseInt(chat, 10, 64); err == nil {
		return tgbotapi.NewMessage(id, text)
	}
	return tgbotapi.NewMessageToChannel(chat, text)
}

// SendPhoto downloads the image from photoURL and sends it to Telegram as file
// bytes, avoiding Telegram-server-side URL fetching failures.
func (b *Bot) SendPhoto(chat, photoURL, caption string) error {
	data, ct, err := photoFetcher.Get(context.Background(), photoURL)
	if err != nil {
		return fmt.Errorf("download photo: %w", err)
	}
	if !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("telegram: unexpected content-type %q, expected image/*", ct)
	}

	name := path.Base(photoURL)
	if name == "" || name == "." {
		name = "photo.jpg"
	}

	file := tgbotapi.FileBytes{Name: name, Bytes: data}
	var photo tgbotapi.PhotoConfig
	if id, perr := strconv.ParseInt(chat, 10, 64); perr == nil {
		photo = tgbotapi.NewPhoto(id, file)
	} else {
		photo = tgbotapi.NewPhotoToChannel(chat, file)
	}
	photo.Caption = caption
	_, err = b.api.Send(photo)
	return sanitizeTokenError(err, b.token)
}

// SendReply sends a text message as a reply to a specific message in a chat.
// chat is a numeric ID or a public @channelusername.
func (b *Bot) SendReply(chat string, messageID int, text string) error {
	msg := newTextMessage(chat, text)
	msg.ReplyToMessageID = messageID
	_, err := b.api.Send(msg)
	return sanitizeTokenError(err, b.token)
}

// GetReviews fetches recent messages received by the bot (direct messages and
// channel comments) and returns them as review-like entries.
// Telegram has no star-rating concept, so rating is always 0.
func (b *Bot) GetReviews(limit int) ([]map[string]interface{}, error) {
	const batchSize = 100
	if limit <= 0 {
		limit = defaultReviewLimit
	}

	var allUpdates []tgbotapi.Update
	offset := 0
	for {
		var batch []tgbotapi.Update
		err := retryTransient(defaultBotRetryAttempts, defaultBotRetryDelay, func() error {
			var e error
			batch, e = b.api.GetUpdates(tgbotapi.UpdateConfig{
				Offset:         offset,
				Limit:          batchSize,
				AllowedUpdates: allowedUpdateTypes,
			})
			return e
		})
		if err != nil {
			if len(allUpdates) == 0 {
				return nil, sanitizeTokenError(fmt.Errorf("get updates: %w", err), b.token)
			}
			break
		}
		if len(batch) == 0 {
			break
		}
		allUpdates = append(allUpdates, batch...)
		offset = batch[len(batch)-1].UpdateID + 1
		if len(allUpdates) >= limit {
			break
		}
	}

	if offset > 0 {
		_, _ = b.api.GetUpdates(tgbotapi.UpdateConfig{Offset: offset, Limit: 1})
	}

	reviews := make([]map[string]interface{}, 0, len(allUpdates))
	for _, u := range allUpdates {
		msg := u.Message
		if msg == nil {
			msg = u.ChannelPost
		}
		if msg == nil {
			continue
		}

		if msg.IsAutomaticForward {
			continue
		}
		if msg.SenderChat != nil && msg.Chat != nil && msg.SenderChat.ID == msg.Chat.ID {
			continue
		}

		text := msg.Text
		if text == "" {
			text = msg.Caption
		}
		if text == "" {
			text = mediaSummary(msg)
		}
		if text == "" {
			continue
		}

		author := ""
		if msg.From != nil {
			author = msg.From.FirstName
			if msg.From.LastName != "" {
				author += " " + msg.From.LastName
			}
			if author == "" {
				author = msg.From.UserName
			}
		}
		if author == "" && msg.SenderChat != nil {
			author = msg.SenderChat.Title
		}
		if author == "" && msg.Chat != nil {
			author = msg.Chat.Title
		}

		if msg.Chat == nil {
			continue
		}

		review := map[string]interface{}{
			"id":         fmt.Sprintf("%d_%d", msg.Chat.ID, msg.MessageID),
			"message_id": msg.MessageID,
			"chat_id":    msg.Chat.ID,
			"author":     author,
			"rating":     0,
			"text":       text,
			"reply":      "",
			"created_at": time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339),
		}

		if r := msg.ReplyToMessage; r != nil && r.IsAutomaticForward {
			if r.SenderChat != nil {
				review["channel_id"] = r.SenderChat.ID
			}
			review["channel_post_id"] = r.ForwardFromMessageID
		}

		reviews = append(reviews, review)
	}
	return reviews, nil
}

// CallbackEvent is the subset of a Telegram callback_query the approval plane
// needs. FromID is callback_query.from.id — the tapper's user id, which Telegram
// guarantees is authentic (the api-side consumer binds it to the batch business's
// verified owner id). Data is the opaque callback_data set on the button.
// QueryID and ChatID scope the answerCallbackQuery ack + the follow-up toast.
type CallbackEvent struct {
	QueryID string
	FromID  int64
	Data    string
	ChatID  int64
}

// PollCallbacks long-polls GetUpdates for callback_query updates only and
// invokes onCallback for each. It runs until ctx is canceled, then returns
// ctx.Err(). The offset is advanced past each processed update so a callback is
// delivered at most once across restarts of the loop within a process. Poll
// errors are logged and retried after a short backoff rather than terminating
// the loop, so a transient Bot API blip does not disable the approval plane. The
// allowed-updates set is callbackUpdateTypes ONLY, so this poll never competes
// with the review poll for plain messages.
func (b *Bot) PollCallbacks(ctx context.Context, onCallback func(cq CallbackEvent)) error {
	offset := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var batch []tgbotapi.Update
		err := retryTransient(defaultBotRetryAttempts, defaultBotRetryDelay, func() error {
			var e error
			batch, e = b.api.GetUpdates(tgbotapi.UpdateConfig{
				Offset:         offset,
				Timeout:        callbackPollTimeout,
				AllowedUpdates: callbackUpdateTypes,
			})
			return e
		})
		if err != nil {
			slog.Warn("telegram agent: callback poll failed, retrying", "error", sanitizeTokenError(err, b.token))
			if !sleepCtx(ctx, defaultBotRetryDelay) {
				return ctx.Err()
			}
			continue
		}
		for i := range batch {
			u := batch[i]
			offset = u.UpdateID + 1
			cq := u.CallbackQuery
			if cq == nil || cq.From == nil {
				continue
			}
			onCallback(CallbackEvent{
				QueryID: cq.ID,
				FromID:  cq.From.ID,
				Data:    cq.Data,
				ChatID:  callbackChatID(cq),
			})
		}
	}
}

// callbackChatID returns the chat id the callback originated from, or 0 when the
// originating message is absent (inline-mode callbacks). It is used only to scope
// the answerCallbackQuery toast, never for authorization.
func callbackChatID(cq *tgbotapi.CallbackQuery) int64 {
	if cq.Message != nil && cq.Message.Chat != nil {
		return cq.Message.Chat.ID
	}
	return 0
}

// AnswerCallback acknowledges a callback_query so Telegram stops the button
// spinner. text is the optional toast shown to the tapper; alert=true renders it
// as a modal alert instead of a transient toast. An empty text sends a bare ack.
func (b *Bot) AnswerCallback(callbackQueryID, text string, alert bool) error {
	cb := tgbotapi.NewCallback(callbackQueryID, text)
	cb.ShowAlert = alert
	_, err := b.api.Request(cb)
	return sanitizeTokenError(err, b.token)
}

// sleepCtx sleeps for d unless ctx is canceled first. It reports true if the
// full delay elapsed and false if ctx was canceled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// mediaSummary returns a short placeholder describing a non-text message so
// that media comments (photo, sticker, voice, etc.) can still be stored as
// a review entry instead of being silently dropped.
func mediaSummary(msg *tgbotapi.Message) string {
	switch {
	case len(msg.Photo) > 0:
		return "[photo]"
	case msg.Video != nil:
		return "[video]"
	case msg.VideoNote != nil:
		return "[video note]"
	case msg.Voice != nil:
		return fmt.Sprintf("[voice %ds]", msg.Voice.Duration)
	case msg.Audio != nil:
		return "[audio]"
	case msg.Animation != nil:
		return "[gif]"
	case msg.Sticker != nil:
		if msg.Sticker.Emoji != "" {
			return "[sticker " + msg.Sticker.Emoji + "]"
		}
		return "[sticker]"
	case msg.Document != nil:
		return "[document]"
	case msg.Contact != nil:
		return "[contact]"
	case msg.Location != nil:
		return "[location]"
	case msg.Poll != nil:
		return "[poll]"
	}
	return ""
}
