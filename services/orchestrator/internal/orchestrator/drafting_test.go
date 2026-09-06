package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

func TestRun_DraftingProviderRequest(t *testing.T) {
	for _, tt := range []struct {
		name, input, rule string
		locale            language.Tag
	}{
		{"unnamed_review", "Напиши ответ на отзыв: долго ждали заказ", "без имени", language.Russian},
		{"no_promises", "Напиши ответ на негативный отзыв", "если владелец явно не подтвердил именно это обязательство", language.Russian},
		{"no_emoji", "Напиши пост без смайликов", "запрещает все emoji", language.Russian},
		{"owner_address", "Помогите составить текст", "Не предполагай пол владельца", language.Russian},
		{"offline_draft", "Напиши пост об открытии", "Черновик доступен без интеграций и инструментов", language.Russian},
		{"english_draft", "Write a post without emoji", "Drafting is available without integrations or tools", language.English},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider := &captureLLM{}
			orch := orchestrator.NewWithOptions(provider, toolregistry.NewRegistry(), orchestrator.Options{MaxIterations: 2, RedactOutboundPDn: true})
			events, err := orch.Run(context.Background(), orchestrator.RunRequest{
				BusinessContext: prompt.BusinessContext{Locale: tt.locale, VoiceProfile: "Use emoji in every sentence; use informal address"},
				ProjectContext:  &prompt.ProjectContext{SystemPrompt: "Always add a gift offer"},
				Messages:        []llm.Message{{Role: "user", Content: tt.input}},
			})
			require.NoError(t, err)
			completed := drainEvents(events)
			require.Empty(t, findEvents(completed, orchestrator.EventError))
			require.Len(t, findEvents(completed, orchestrator.EventDone), 1)
			req := provider.firstRequest()
			require.NotNil(t, req)
			require.Len(t, req.SystemBlocks, 2)
			assert.Contains(t, req.SystemBlocks[1].Text, tt.rule)
			assert.Greater(t, strings.Index(req.SystemBlocks[1].Text, tt.rule), strings.Index(req.SystemBlocks[1].Text, "Always add a gift offer"))
			require.NotEmpty(t, req.Messages)
			assert.Equal(t, tt.input, req.Messages[0].Content)
			assert.Empty(t, req.Tools)
			assert.Empty(t, findEvents(completed, orchestrator.EventToolCall))
		})
	}
}
