package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
)

// Only the URL methods are used by ownership validation.
type photoUploadStore struct{ storage.Uploader }

func (photoUploadStore) PublicURL(key string) string { return "/media/" + key }
func (photoUploadStore) KeyFromPublicURL(url string) string {
	return strings.TrimPrefix(url, "/media/")
}

func TestHITLPhotoEditsRequireOwnedUpload(t *testing.T) {
	for _, tool := range []string{tools.TelegramSendChannelPhoto, tools.VKPostPhoto} {
		t.Run(tool, func(t *testing.T) {
			businessID := uuid.NewString()
			own := "/media/businesses/" + businessID + "/media/" + uuid.NewString() + ".png"
			generated := "https://image-provider.example/generated.png"
			for _, tc := range []struct {
				name, url      string
				noStore, valid bool
			}{
				{name: "owned", url: own, valid: true},
				{name: "foreign", url: strings.Replace(own, businessID, uuid.NewString(), 1)},
				{name: "remote", url: "https://external.example/image.png"},
				{name: "traversal", url: own + "/../other.png"},
				{name: "encoded", url: "/media/businesses/" + businessID + "/media/%2e%2e.png"},
				{name: "malformed", url: strings.Replace(own, ".png", ".gif", 1)},
				{name: "missing store", url: own, noStore: true},
				{name: "unchanged generated", url: generated, noStore: true, valid: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					pending := newStubPendingRepo()
					seedBatch(pending, "batch", "conversation", businessID, []domain.PendingCall{{CallID: "photo", ToolName: tool, Arguments: map[string]interface{}{"photo_url": generated, "caption": "Original", "channel_id": "kept", "owner_id": "kept"}}})
					svc := newSvc(t, pending, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(businessID)}}, &stubProjectRepo{})
					svc.ToolsCache().Seed([]service.ToolsRegistryEntry{{Name: tool, Platform: "photo", Floor: domain.ToolFloorManual, EditableFields: []string{"caption", "photo_url"}}})
					if !tc.noStore {
						svc.SetAttachmentStorage(photoUploadStore{})
					}
					_, err := svc.Resolve(context.Background(), service.ResolveInput{ConversationID: "conversation", BatchID: "batch", ActorBusinessID: businessID, Decisions: []service.DecisionInput{{ID: "photo", Action: "edit", EditedArgs: map[string]interface{}{"photo_url": tc.url, "caption": "Edited"}}}})
					if !tc.valid {
						require.ErrorIs(t, err, service.ErrHITLInvalidPhotoURL)
						require.Zero(t, pending.ResolvingCounter)
						require.Empty(t, pending.RecordedDecisions)
						return
					}
					require.NoError(t, err)
					require.Equal(t, tc.url, pending.RecordedDecisions[0].EditedArgs["photo_url"])
					require.Equal(t, "kept", pending.RecordedDecisions[0].Arguments["channel_id"])
					require.Equal(t, domain.ToolFloorManual, pending.RecordedDecisions[0].FloorAtPause)
				})
			}
		})
	}
}
