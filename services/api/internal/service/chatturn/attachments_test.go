package chatturn

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type attachmentMessages struct {
	fakeMessageRepo
	history []domain.Message
}

func (m *attachmentMessages) ListByConversationID(context.Context, string, int, int) ([]domain.Message, error) {
	return m.history, nil
}

func TestAttachmentsPersistSeparatelyAndRemainInFollowUpContext(t *testing.T) {
	businessID := uuid.New()
	conv := &ownershipConvRepo{conv: &domain.Conversation{ID: "conv"}}
	messages := &attachmentMessages{}
	turn := newOwnershipTurn(conv, messages)
	turn.SetAttachmentPublicURL("https://onevoice.example")
	attachment := domain.Attachment{Type: "image", URL: "/media/businesses/" + businessID.String() + "/media/photo.png", MimeType: "image/png", Name: "not model instructions"}
	req := TurnRequest{BusinessID: businessID, ConversationID: "conv", Message: "use this photo", Attachments: []domain.Attachment{attachment}}
	enriched, err := turn.enrich(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, turn.persistUserMessage(context.Background(), enriched.userMessage))
	require.Len(t, messages.created, 1)
	require.Equal(t, req.Message, messages.created[0].Content)
	require.Equal(t, req.Attachments, messages.created[0].Attachments)
	body := turn.buildOrchestratorRequest(context.Background(), req, enriched)
	require.Contains(t, body["message"], "https://onevoice.example/media/")
	require.NotContains(t, body["message"], attachment.Name)
	messages.history = append(messages.history, messages.created...)
	messages.history = append(messages.history, domain.Message{Role: domain.MessageRoleAssistant, Attachments: []domain.Attachment{attachment}}, domain.Message{Role: domain.MessageRoleAssistant})
	followUp := turn.loadHistory(context.Background(), "conv")
	require.Len(t, followUp, 2)
	require.Contains(t, followUp[0]["content"], "https://onevoice.example/media/")
	require.Equal(t, body["message"], followUp[0]["content"])
	require.Contains(t, followUp[1]["content"], "https://onevoice.example/media/")
	require.Equal(t, "plain", contentWithAttachments("plain", nil, "https://onevoice.example"))
	require.Equal(t, "plain", contentWithAttachments("plain", []domain.Attachment{{Type: "file", URL: "ignored"}}, "https://onevoice.example"))
}
