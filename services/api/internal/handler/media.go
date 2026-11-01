package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
)

const mediaMultipartOverhead = 64 << 10

// UploadMedia stores a fresh image without changing the business profile or logo.
func (h *BusinessHandler) UploadMedia(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UploadMedia", authz.PermContentCreate)
	if !ok {
		return
	}
	if h.storage == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "storage_unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+mediaMultipartOverhead)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_or_oversized_file")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 || header.Size > maxUploadSize {
		writeJSONError(w, http.StatusBadRequest, "invalid_or_oversized_file")
		return
	}
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid_file")
		return
	}
	mimeType := http.DetectContentType(buf[:n])
	ext, ok := mediaExtension(mimeType)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "unsupported_file_type")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "upload_failed")
		return
	}
	if _, err := h.businessService.GetByID(r.Context(), bc.BusinessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business_not_found")
		} else {
			slog.ErrorContext(r.Context(), "media upload: business lookup failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "upload_failed")
		}
		return
	}
	key := fmt.Sprintf("businesses/%s/media/%s%s", bc.BusinessID, uuid.NewString(), ext)
	if err := h.storage.Upload(r.Context(), key, file, header.Size, mimeType); err != nil {
		slog.ErrorContext(r.Context(), "media upload failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "upload_failed")
		return
	}
	writeJSON(w, http.StatusCreated, domain.Attachment{Type: "image", URL: h.storage.PublicURL(key), MimeType: mimeType, Name: header.Filename})
}

func mediaExtension(mimeType string) (string, bool) {
	ext, ok := allowedMimeTypes[mimeType]
	return ext, ok && (mimeType == "image/jpeg" || mimeType == "image/png" || mimeType == "image/webp")
}

// SetAttachmentStorage wires the existing upload store and configured public origin.
// Relative local upload URLs stay relative in stored messages; only model context resolves them.
func (h *ChatProxyHandler) SetAttachmentStorage(store storage.Uploader, publicURL string) {
	h.attachmentStorage = store
	h.turn.SetAttachmentPublicURL(publicURL)
}

func (h *ChatProxyHandler) validateAttachments(businessID uuid.UUID, input []openapi.MessageAttachment) ([]domain.Attachment, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if len(input) > 1 || h.attachmentStorage == nil {
		return nil, fmt.Errorf("one uploaded image is required")
	}
	a := input[0]
	if err := validate.Struct(a); err != nil {
		return nil, fmt.Errorf("invalid attachment fields")
	}
	ext, ok := mediaExtension(a.MimeType)
	if a.Type != "image" || !ok {
		return nil, fmt.Errorf("unsupported attachment")
	}
	key := h.attachmentStorage.KeyFromPublicURL(a.Url)
	prefix := "businesses/" + businessID.String() + "/media/"
	if !strings.HasPrefix(key, prefix) || a.Url != h.attachmentStorage.PublicURL(key) {
		return nil, fmt.Errorf("attachment outside business media")
	}
	filename := strings.TrimPrefix(key, prefix)
	if strings.ContainsAny(filename, "/\\%?#") || path.Ext(filename) != ext {
		return nil, fmt.Errorf("invalid media key")
	}
	if _, err := uuid.Parse(strings.TrimSuffix(filename, ext)); err != nil {
		return nil, fmt.Errorf("invalid media key")
	}
	return []domain.Attachment{{Type: "image", URL: a.Url, MimeType: a.MimeType, Name: a.Name}}, nil
}
