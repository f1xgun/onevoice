package wire

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

func TestPostMutationToolsRequireManualApproval(t *testing.T) {
	registry := toolregistry.NewRegistry()
	RegisterPlatformTools(registry, nil, false)
	for _, name := range []string{tools.TelegramEditChannelPost, tools.TelegramDeleteChannelPost, tools.VKEditPost, tools.VKDeletePost} {
		t.Run(name, func(t *testing.T) {
			require.True(t, registry.Has(name))
			require.Equal(t, domain.ToolFloorManual, registry.Floor(name))
			for _, field := range registry.EditableFields(name) {
				require.Equal(t, "text", field, "approval edits cannot retarget a post")
			}
			for _, definition := range registry.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, domain.WhitelistModeExplicit, nil) {
				require.NotEqual(t, name, definition.Function.Name)
			}
			available := registry.AvailableForWhitelist(context.Background(), []string{"telegram", "vk"}, domain.WhitelistModeExplicit, []string{name})
			found := false
			for _, definition := range available {
				if definition.Function.Name == name {
					found = true
				}
			}
			require.True(t, found)
		})
	}
}
