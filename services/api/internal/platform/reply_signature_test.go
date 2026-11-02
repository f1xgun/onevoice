package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplySignatureSettings(t *testing.T) {
	legacy := "Не обещать скидки.\nКак подписывать ответы: Анна, управляющая"
	body, signature := SplitLegacyVoiceProfile(legacy)
	assert.Equal(t, "Не обещать скидки.", body)
	assert.Equal(t, "Анна, управляющая", signature)
	for _, prefix := range legacySignaturePrefixes {
		_, got := SplitLegacyVoiceProfile("Описание\n" + prefix + " Anna")
		assert.Equal(t, "Anna", got)
	}
	settings := map[string]interface{}{VoiceProfileSettingsKey: legacy}
	assert.Equal(t, signature, ReplySignatureFromSettings(settings))
	assert.Equal(t, legacy, VoiceInstructionsFromSettings(settings))
	settings[ReplySignatureSettingsKey] = "Команда Зёрен"
	assert.Equal(t, "Команда Зёрен", ReplySignatureFromSettings(settings))
	assert.Equal(t, "Не обещать скидки.\nКак подписывать ответы: Команда Зёрен", VoiceInstructionsFromSettings(settings))
	settings[ReplySignatureSettingsKey] = ""
	assert.Empty(t, ReplySignatureFromSettings(settings))
	assert.Equal(t, "Не обещать скидки.", VoiceInstructionsFromSettings(settings))
	assert.Empty(t, ReplySignatureFromSettings(nil))
	assert.Empty(t, VoiceInstructionsFromSettings(nil))
	profile := "  Описание.\nБез эмодзи.  "
	assert.Equal(t, profile, VoiceInstructionsFromSettings(map[string]interface{}{VoiceProfileSettingsKey: profile}))
}
