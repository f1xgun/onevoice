package telegram

import (
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

// SendPhoto sends a photo to the given chat ID using a public URL.
func (b *Bot) SendPhoto(chatID int64, photoURL, caption string) error {
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(photoURL))
	photo.Caption = caption
	_, err := b.api.Send(photo)
	return err
}
