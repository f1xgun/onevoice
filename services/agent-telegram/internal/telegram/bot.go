package telegram

import (
	"fmt"
	"io"
	"net/http"
	"path"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
	resp, err := http.Get(photoURL) //nolint:noctx
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
