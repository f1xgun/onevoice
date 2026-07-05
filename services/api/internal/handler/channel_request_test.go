package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type fakeChannelRequestService struct {
	recordCalled int
	gotBusiness  uuid.UUID
	gotInput     service.ChannelRequestInput
	recordErr    error
	summary      []service.ChannelDemandCount
	summaryErr   error
}

func (f *fakeChannelRequestService) Record(_ context.Context, businessID uuid.UUID, in service.ChannelRequestInput) error {
	f.recordCalled++
	f.gotBusiness = businessID
	f.gotInput = in
	return f.recordErr
}

func (f *fakeChannelRequestService) Summary(_ context.Context, businessID uuid.UUID) ([]service.ChannelDemandCount, error) {
	f.gotBusiness = businessID
	return f.summary, f.summaryErr
}

func channelCreateCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentCreate},
	})
}

func channelReadCtx(businessID, userID uuid.UUID) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	})
}

func postChannelRequest(ctx context.Context, t *testing.T, h *ChannelRequestHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/businesses/x/channel-requests", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

func TestChannelRequest_Create_Valid(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	bizID := uuid.New()
	body, _ := json.Marshal(map[string]any{"channel": "avito", "note": "нужен Авито"})

	w := postChannelRequest(channelCreateCtx(bizID, uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, fake.recordCalled)
	assert.Equal(t, bizID, fake.gotBusiness)
	assert.Equal(t, "avito", fake.gotInput.Channel)
	assert.Equal(t, "нужен Авито", fake.gotInput.Note)
}

func TestChannelRequest_Create_UnknownChannelRejected(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "tiktok"})

	w := postChannelRequest(channelCreateCtx(uuid.New(), uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.recordCalled, "unknown channel must not reach the service")
}

func TestChannelRequest_Create_2gisAccepted(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "2gis"})

	w := postChannelRequest(channelCreateCtx(uuid.New(), uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, fake.recordCalled)
	assert.Equal(t, "2gis", fake.gotInput.Channel)
}

func TestChannelRequest_Create_NoteTooLong(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "ozon", "note": strings.Repeat("a", 281)})

	w := postChannelRequest(channelCreateCtx(uuid.New(), uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.recordCalled)
}

func TestChannelRequest_Create_CyrillicNoteAtCapAccepted(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "other", "note": strings.Repeat("я", 280)})

	w := postChannelRequest(channelCreateCtx(uuid.New(), uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, fake.recordCalled)
}

func TestChannelRequest_Create_MissingPermForbidden(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "avito"})
	w := postChannelRequest(channelReadCtx(uuid.New(), uuid.New()), t, h, string(body))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, fake.recordCalled)
}

func TestChannelRequest_Create_NoBusinessContext(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)
	body, _ := json.Marshal(map[string]any{"channel": "avito"})

	w := postChannelRequest(context.Background(), t, h, string(body))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, fake.recordCalled)
}

func TestChannelRequest_List_Aggregate(t *testing.T) {
	bizID := uuid.New()
	fake := &fakeChannelRequestService{
		summary: []service.ChannelDemandCount{
			{Channel: "avito", Count: 3},
			{Channel: "ozon", Count: 1},
		},
	}
	h := NewChannelRequestHandler(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses/x/channel-requests", http.NoBody)
	req = req.WithContext(channelReadCtx(bizID, uuid.New()))
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, bizID, fake.gotBusiness, "list must be scoped to the caller's business")
	var resp openapi.ChannelDemandSummary
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Channels, 2)
	assert.Equal(t, openapi.ChannelRequestChannel("avito"), resp.Channels[0].Channel)
	assert.Equal(t, 3, resp.Channels[0].Count)
}

func TestChannelRequest_List_MissingPermForbidden(t *testing.T) {
	fake := &fakeChannelRequestService{}
	h := NewChannelRequestHandler(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/businesses/x/channel-requests", http.NoBody)
	req = req.WithContext(channelCreateCtx(uuid.New(), uuid.New()))
	w := httptest.NewRecorder()
	h.List(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
