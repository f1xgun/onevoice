package telegram

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// photoHTTPClient is used for downloading images from user-provided URLs.
// TLS verification is skipped because external image URLs may use self-signed
// or corp-CA certificates not present in the container trust store.
var photoHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402: intentional, external image hosts may use self-signed certs
	},
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
	err := retryTransient(3, 500*time.Millisecond, func() error {
		var e error
		api, e = tgbotapi.NewBotAPI(token)
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
	redacted := strings.ReplaceAll(msg, token, "***REDACTED_BOT_TOKEN***")
	if redacted == msg {
		return err
	}
	return errors.New(redacted)
}

// SendMessage sends a text message to the given chat ID.
func (b *Bot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return sanitizeTokenError(err, b.token)
}

// SendPhoto downloads the image from photoURL and sends it to Telegram as file
// bytes, avoiding Telegram-server-side URL fetching failures.
func (b *Bot) SendPhoto(chatID int64, photoURL, caption string) error {
	resp, err := photoHTTPClient.Get(photoURL)
	if err != nil {
		return fmt.Errorf("download photo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download photo: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read photo body: %w", err)
	}

	name := path.Base(photoURL)
	if name == "" || name == "." {
		name = "photo.jpg"
	}

	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: name, Bytes: data})
	photo.Caption = caption
	_, err = b.api.Send(photo)
	return sanitizeTokenError(err, b.token)
}

// SendReply sends a text message as a reply to a specific message in a chat.
func (b *Bot) SendReply(chatID int64, messageID int, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
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
		limit = 500
	}

	var allUpdates []tgbotapi.Update
	offset := 0
	for {
		var batch []tgbotapi.Update
		err := retryTransient(3, 500*time.Millisecond, func() error {
			var e error
			batch, e = b.api.GetUpdates(tgbotapi.UpdateConfig{
				Offset:         offset,
				Limit:          batchSize,
				AllowedUpdates: []string{"message", "channel_post", "edited_message", "edited_channel_post"},
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
		reviews = append(reviews, review)
	}
	return reviews, nil
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
