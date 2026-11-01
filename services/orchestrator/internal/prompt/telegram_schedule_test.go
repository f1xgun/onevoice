package prompt_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

func TestFutureTelegramPostsCannotUseImmediateTools(t *testing.T) {
	for _, tc := range []struct {
		locale language.Tag
		phrase string
	}{{language.English, "Never omit the time or send it immediately"}, {language.Russian, "не отправляй пост немедленно"}} {
		t.Run(tc.locale.String(), func(t *testing.T) {
			_, business, _ := prompt.BuildSplit(prompt.BusinessContext{Locale: tc.locale}, nil, nil)
			require.Contains(t, business, tc.phrase)
		})
	}
}
