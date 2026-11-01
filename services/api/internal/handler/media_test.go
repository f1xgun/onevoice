package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

func TestUploadMediaUsesFreshKeyAndNeverMutatesLogo(t *testing.T) {
	for _, size := range []int{len(pngMagic), maxUploadSize} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			businessID := uuid.New()
			svc := new(MockBusinessService)
			store := new(mockUploader)
			svc.On("GetByID", mock.Anything, businessID).Return(&domain.Business{ID: businessID, LogoURL: "existing-logo"}, nil)
			var storedKey string
			prefix := "businesses/" + businessID.String() + "/media/"
			store.On("Upload", mock.Anything, mock.MatchedBy(func(key string) bool {
				storedKey = key
				return strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".png") && !strings.Contains(key, "logo")
			}), mock.MatchedBy(func(data []byte) bool { return len(data) == size }), int64(size), "image/png").Return(nil)
			store.On("PublicURL", mock.Anything).Return("/media/attachment.png")
			h, err := NewBusinessHandler(svc, nil, store)
			require.NoError(t, err)
			image := append(append([]byte{}, pngMagic...), make([]byte, size-len(pngMagic))...)
			body, contentType := buildLogoMultipart(t, image)
			req := httptest.NewRequest(http.MethodPost, "/media", bytes.NewReader(bytes.Replace(body.Bytes(), []byte(`name="logo"`), []byte(`name="file"`), 1)))
			req.Header.Set("Content-Type", contentType)
			req = withBizCtx(req, authz.BusinessContext{BusinessID: businessID, UserID: uuid.New(), Permissions: []authz.Permission{authz.PermContentCreate}})
			w := httptest.NewRecorder()
			h.UploadMedia(w, req)
			require.Equal(t, http.StatusCreated, w.Code)
			var attachment domain.Attachment
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &attachment))
			require.Equal(t, "image", attachment.Type)
			require.Equal(t, "image/png", attachment.MimeType)
			require.Equal(t, "logo.png", attachment.Name)
			_, err = uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(storedKey, prefix), ".png"))
			require.NoError(t, err)
			svc.AssertNotCalled(t, "UpdateLogoURL", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			store.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
			svc.AssertExpectations(t)
			store.AssertExpectations(t)
		})
	}
}

func TestUploadMediaRejectsInvalidFilesAndMissingPermission(t *testing.T) {
	cases := []struct {
		name       string
		data       []byte
		permission bool
		want       int
	}{
		{"text", []byte("not an image"), true, http.StatusBadRequest},
		{"gif", []byte("GIF89a"), true, http.StatusBadRequest},
		{"too large", append(pngMagic, make([]byte, maxUploadSize)...), true, http.StatusBadRequest},
		{"empty", nil, true, http.StatusBadRequest},
		{"forbidden", pngMagic, false, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(MockBusinessService)
			store := new(mockUploader)
			h, err := NewBusinessHandler(svc, nil, store)
			require.NoError(t, err)
			body, contentType := buildLogoMultipart(t, tc.data)
			req := httptest.NewRequest(http.MethodPost, "/media", bytes.NewReader(bytes.Replace(body.Bytes(), []byte(`name="logo"`), []byte(`name="file"`), 1)))
			req.Header.Set("Content-Type", contentType)
			bc := authz.BusinessContext{BusinessID: uuid.New(), UserID: uuid.New()}
			if tc.permission {
				bc.Permissions = []authz.Permission{authz.PermContentCreate}
			}
			req = withBizCtx(req, bc)
			w := httptest.NewRecorder()
			h.UploadMedia(w, req)
			require.Equal(t, tc.want, w.Code)
			require.Empty(t, store.Calls)
			require.Empty(t, svc.Calls)
		})
	}
}

func TestChatAttachmentsAreOwnedAndRejectedBeforeTurn(t *testing.T) {
	businessID := uuid.New()
	filename := uuid.NewString() + ".png"
	key := "businesses/" + businessID.String() + "/media/" + filename
	cases := []struct {
		name, url, key, mime, kind string
		valid                      bool
	}{
		{"own", "/media/" + key, key, "image/png", "image", true},
		{"foreign business", "/media/businesses/" + uuid.NewString() + "/media/" + filename, "businesses/foreign/media/" + filename, "image/png", "image", false},
		{"remote", "https://foreign.example/" + key, "", "image/png", "image", false},
		{"traversal", "/media/" + key + "/../other.png", key + "/../other.png", "image/png", "image", false},
		{"encoded traversal", "/media/businesses/" + businessID.String() + "/media/%2e%2e.png", "businesses/" + businessID.String() + "/media/%2e%2e.png", "image/png", "image", false},
		{"mismatched mime", "/media/" + key, key, "image/jpeg", "image", false},
		{"wrong type", "/media/" + key, key, "image/png", "file", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := new(mockUploader)
			store.On("KeyFromPublicURL", tc.url).Return(tc.key).Maybe()
			store.On("PublicURL", tc.key).Return(tc.url).Maybe()
			h := &ChatProxyHandler{attachmentStorage: store}
			attachments := []openapi.MessageAttachment{{Type: tc.kind, Url: tc.url, MimeType: tc.mime, Name: "image.png"}}
			got, err := h.validateAttachments(businessID, attachments)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.url, got[0].URL)
				return
			}
			require.Error(t, err)
			payload, err := json.Marshal(map[string]interface{}{"message": "use this image", "attachments": attachments})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/chat/conversation", bytes.NewReader(payload))
			req = withBizCtx(req, authz.BusinessContext{BusinessID: businessID, UserID: uuid.New(), Permissions: []authz.Permission{authz.PermContentCreate}})
			w := httptest.NewRecorder()
			h.Chat(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
	store := new(mockUploader)
	h := &ChatProxyHandler{attachmentStorage: store}
	_, err := h.validateAttachments(businessID, []openapi.MessageAttachment{{Type: "image", Url: "/media/" + key, MimeType: "image/png"}})
	require.Error(t, err)
	require.Empty(t, store.Calls)
	_, err = h.validateAttachments(businessID, make([]openapi.MessageAttachment, 2))
	require.Error(t, err)
	require.Empty(t, store.Calls)
}

func TestUploadMediaDoesNotHideLookupFailureAsMissingBusiness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"missing", domain.ErrBusinessNotFound, http.StatusNotFound}, {"unavailable", errors.New("offline"), http.StatusInternalServerError}} {
		t.Run(tc.name, func(t *testing.T) {
			businessID := uuid.New()
			svc := new(MockBusinessService)
			store := new(mockUploader)
			svc.On("GetByID", mock.Anything, businessID).Return((*domain.Business)(nil), tc.err)
			h, err := NewBusinessHandler(svc, nil, store)
			require.NoError(t, err)
			body, contentType := buildLogoMultipart(t, pngMagic)
			req := httptest.NewRequest(http.MethodPost, "/media", bytes.NewReader(bytes.Replace(body.Bytes(), []byte(`name="logo"`), []byte(`name="file"`), 1)))
			req.Header.Set("Content-Type", contentType)
			req = withBizCtx(req, authz.BusinessContext{BusinessID: businessID, UserID: uuid.New(), Permissions: []authz.Permission{authz.PermContentCreate}})
			w := httptest.NewRecorder()
			h.UploadMedia(w, req)
			require.Equal(t, tc.status, w.Code)
			require.Empty(t, store.Calls)
		})
	}
}
