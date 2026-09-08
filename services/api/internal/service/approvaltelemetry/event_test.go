package approvaltelemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestKind_CoversEveryApprovalPublicationTool(t *testing.T) {
	tests := map[string]string{
		tools.TelegramSendChannelPost:   "post",
		tools.TelegramSendChannelPhoto:  "post",
		tools.VKPublishPost:             "post",
		tools.VKPostPhoto:               "post",
		tools.VKSchedulePost:            "post",
		tools.YandexBusinessCreatePost:  "post",
		tools.TelegramReplyToComment:    "review_reply",
		tools.VKReplyComment:            "review_reply",
		tools.YandexBusinessReplyReview: "review_reply",
		tools.GoogleBusinessReplyReview: "review_reply",
	}
	for toolName, want := range tests {
		require.Equal(t, want, Kind(toolName), toolName)
	}
	require.Empty(t, Kind(tools.YandexBusinessUpdateHours))
	require.Empty(t, Kind("private@example.com"))
}
