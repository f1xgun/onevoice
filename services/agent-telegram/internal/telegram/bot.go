package telegram

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"path"
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
	api *tgbotapi.BotAPI
}

// New creates a Bot with the given token.
func New(token string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	return &Bot{api: api}, nil
}

// SendMessage sends a text message to the given chat ID.
func (b *Bot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
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
	return err
}

// SendReply sends a text message as a reply to a specific message in a chat.
func (b *Bot) SendReply(chatID int64, messageID int, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyToMessageID = messageID
	_, err := b.api.Send(msg)
	return err
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
		batch, err := b.api.GetUpdates(tgbotapi.UpdateConfig{
			Offset:         offset,
			Limit:          batchSize,
			AllowedUpdates: []string{"message", "channel_post", "edited_message", "edited_channel_post"},
		})
		if err != nil {
			if len(allUpdates) == 0 {
				return nil, fmt.Errorf("get updates: %w", err)
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
		if msg == nil || msg.Text == "" {
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
		if author == "" && msg.Chat != nil {
			author = msg.Chat.Title
		}

		review := map[string]interface{}{
			"id":         fmt.Sprintf("%d_%d", msg.Chat.ID, msg.MessageID),
			"message_id": msg.MessageID,
			"chat_id":    msg.Chat.ID,
			"author":     author,
			"rating":     0,
			"text":       msg.Text,
			"reply":      "",
			"created_at": time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339),
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}
