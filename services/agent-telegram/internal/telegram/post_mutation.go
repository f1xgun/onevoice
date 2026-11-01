package telegram

import (
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// EditMessage changes text or a media caption without replacing the message.
func (b *Bot) EditMessage(chat string, messageID int, text string, caption bool) (SentMessage, error) {
	base := tgbotapi.BaseEdit{MessageID: messageID, ChannelUsername: chat}
	if id, err := strconv.ParseInt(chat, 10, 64); err == nil {
		base.ChatID = id
		base.ChannelUsername = ""
	}
	var edit tgbotapi.Chattable = tgbotapi.EditMessageTextConfig{BaseEdit: base, Text: text}
	if caption {
		edit = tgbotapi.EditMessageCaptionConfig{BaseEdit: base, Caption: text}
	}
	sent, err := b.api.Send(edit)
	if err != nil {
		// A replay after a successful edit has already reached the desired state.
		if strings.Contains(err.Error(), "message is not modified") {
			return SentMessage{MessageID: messageID}, nil
		}
		return SentMessage{}, sanitizeTokenError(err, b.token)
	}
	return sentMessageOf(sent), nil
}

// DeleteMessage deletes a channel message subject to Telegram's 48-hour limit.
func (b *Bot) DeleteMessage(chat string, messageID int) error {
	deletion := tgbotapi.DeleteMessageConfig{MessageID: messageID, ChannelUsername: chat}
	if id, err := strconv.ParseInt(chat, 10, 64); err == nil {
		deletion.ChatID = id
		deletion.ChannelUsername = ""
	}
	_, err := b.api.Request(deletion)
	return sanitizeTokenError(err, b.token)
}
