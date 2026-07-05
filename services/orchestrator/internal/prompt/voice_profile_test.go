package prompt_test

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

// TestBuildSystemPrompt_VoiceProfile_RendersInBusinessBlockRu asserts a set
// VoiceProfile appears in the per-business Block 2 (RU), beside the tone line.
// This is the behavioral guard that fails if the Block-2 threading is reverted.
func TestBuildSystemPrompt_VoiceProfile_RendersInBusinessBlockRu(t *testing.T) {
	profile := "Пиши тепло, без канцелярита. Эмодзи не используем."
	_, business, _ := prompt.BuildSplit(prompt.BusinessContext{
		Name:         "Кофейня Уют",
		Tone:         "дружеский",
		VoiceProfile: profile,
		Locale:       language.Russian,
		Now:          time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}, nil, nil)

	assert.Contains(t, business, profile,
		"a set VoiceProfile must render in the per-business Block 2 (RU)")
	assert.Contains(t, business, "Бренд-голос",
		"the RU voice-profile header must be present when a profile is set")
	assert.Contains(t, business, "Тон общения: дружеский",
		"voiceProfile must not clobber the voiceTone line — both render")
}

// TestBuildSystemPrompt_VoiceProfile_RendersInBusinessBlockEn is the EN twin.
func TestBuildSystemPrompt_VoiceProfile_RendersInBusinessBlockEn(t *testing.T) {
	profile := "Write warmly, no corporate jargon. We never use emoji."
	_, business, _ := prompt.BuildSplit(prompt.BusinessContext{
		Name:         "Cozy Café",
		Tone:         "friendly",
		VoiceProfile: profile,
		Locale:       language.English,
		Now:          time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}, nil, nil)

	assert.Contains(t, business, profile,
		"a set VoiceProfile must render in the per-business Block 2 (EN)")
	assert.Contains(t, business, "Brand voice",
		"the EN voice-profile header must be present when a profile is set")
	assert.Contains(t, business, "Tone: friendly",
		"voiceProfile must not clobber the voiceTone line — both render")
}

// TestBuildSystemPrompt_VoiceProfile_UnsetOmitsHeader asserts that when the
// profile is empty the business block renders exactly as before — no header
// leaks in for a business that never set one.
func TestBuildSystemPrompt_VoiceProfile_UnsetOmitsHeader(t *testing.T) {
	ru := prompt.BusinessContext{Name: "Acme", Locale: language.Russian, Now: time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)}
	en := prompt.BusinessContext{Name: "Acme", Locale: language.English, Now: time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)}

	_, ruBusiness, _ := prompt.BuildSplit(ru, nil, nil)
	_, enBusiness, _ := prompt.BuildSplit(en, nil, nil)

	assert.NotContains(t, ruBusiness, "Бренд-голос",
		"an unset voiceProfile must not emit the RU header")
	assert.NotContains(t, enBusiness, "Brand voice",
		"an unset voiceProfile must not emit the EN header")
}

// TestBuildSystemPrompt_VoiceProfile_ByteIdenticalWhenUnset locks in that
// setting VoiceProfile to "" (or whitespace) yields a business block that is
// byte-identical to the zero-value baseline — existing businesses see no drift.
func TestBuildSystemPrompt_VoiceProfile_ByteIdenticalWhenUnset(t *testing.T) {
	base := prompt.BusinessContext{
		Name:               "Кофейня Уют",
		Category:           "кофейня",
		Tone:               "дружеский",
		ActiveIntegrations: []string{"telegram", "vk"},
		Locale:             language.Russian,
		Now:                time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}
	withBlank := base
	withBlank.VoiceProfile = "   "

	_, baseBusiness, _ := prompt.BuildSplit(base, nil, nil)
	_, blankBusiness, _ := prompt.BuildSplit(withBlank, nil, nil)

	assert.Equal(t,
		sha256.Sum256([]byte(baseBusiness)),
		sha256.Sum256([]byte(blankBusiness)),
		"a blank/whitespace VoiceProfile must leave the business block byte-identical to the no-profile baseline",
	)
}

// TestBuildSystemPrompt_VoiceProfile_NeverLeaksIntoPlatformBlock asserts the
// profile stays out of Block 1 (the cache-locked cross-business prefix). Block 1
// takes no per-business state; a leak here would churn the prompt-cache prefix
// per business and is separately guarded by the Block-1 hash-lock test.
func TestBuildSystemPrompt_VoiceProfile_NeverLeaksIntoPlatformBlock(t *testing.T) {
	profile := "SENTINEL-VOICE-PROFILE-MARKER"
	for _, tag := range []language.Tag{language.Russian, language.English} {
		platform, _, _ := prompt.BuildSplit(prompt.BusinessContext{
			Name:         "Acme",
			VoiceProfile: profile,
			Locale:       tag,
			Now:          time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
		}, nil, nil)
		require.NotContains(t, platform, profile,
			"VoiceProfile must never appear in Block 1 (cache-locked platform prefix)")
	}
}
