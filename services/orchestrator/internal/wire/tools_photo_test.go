package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestPhotoToolsAllowOwnedImageEditsAndKeepManualFloor(t *testing.T) {
	for _, name := range []string{tools.TelegramSendChannelPhoto, tools.VKPostPhoto} {
		spec := findToolSpec(t, append(telegramTools(), vkTools()...), name)
		require.Equal(t, domain.ToolFloorManual, spec.Floor)
		require.Contains(t, spec.EditableFields, "photo_url")
		require.Contains(t, spec.EditableFields, "caption")
		require.NotContains(t, spec.EditableFields, "channel_id")
		require.NotContains(t, spec.EditableFields, "owner_id")
	}
}
