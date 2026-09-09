package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type ContentTemplateHandler struct {
	service *service.ContentTemplateService
}

func NewContentTemplateHandler(svc *service.ContentTemplateService) (*ContentTemplateHandler, error) {
	if svc == nil {
		return nil, fmt.Errorf("NewContentTemplateHandler: service cannot be nil")
	}
	return &ContentTemplateHandler{service: svc}, nil
}

func contentTemplateResponse(t domain.ContentTemplate) openapi.ContentTemplate {
	return openapi.ContentTemplate{
		Id: t.ID, BusinessId: t.BusinessID, CreatedBy: t.CreatedBy, Name: t.Name,
		Kind: openapi.ContentTemplateKind(t.Kind), Body: t.Body, Placeholders: t.Placeholders,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func contentTemplateInput(req openapi.ContentTemplateRequest) service.ContentTemplateInput {
	return service.ContentTemplateInput{Name: req.Name, Kind: domain.ContentTemplateKind(req.Kind), Body: req.Body, Placeholders: req.Placeholders}
}

func (h *ContentTemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListContentTemplates", authz.PermContentRead)
	if !ok {
		return
	}
	templates, err := h.service.List(r.Context(), bc.BusinessID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	out := make([]openapi.ContentTemplate, 0, len(templates))
	for _, template := range templates {
		out = append(out, contentTemplateResponse(template))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ContentTemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	bc, id, ok := templateRequestScope(w, r, "GetContentTemplate", authz.PermContentRead)
	if !ok {
		return
	}
	template, err := h.service.Get(r.Context(), bc.BusinessID, id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contentTemplateResponse(*template))
}

func (h *ContentTemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "CreateContentTemplate", authz.PermContentCreate)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	req, ok := decodeAndValidate[openapi.ContentTemplateRequest](w, r, "invalid_template")
	if !ok {
		return
	}
	template, err := h.service.Create(r.Context(), bc.BusinessID, userID, contentTemplateInput(req))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/businesses/%s/content-templates/%s", bc.BusinessID, template.ID))
	writeJSON(w, http.StatusCreated, contentTemplateResponse(*template))
}

func (h *ContentTemplateHandler) Update(w http.ResponseWriter, r *http.Request) {
	bc, id, ok := templateRequestScope(w, r, "UpdateContentTemplate", authz.PermContentUpdate)
	if !ok {
		return
	}
	req, ok := decodeAndValidate[openapi.ContentTemplateRequest](w, r, "invalid_template")
	if !ok {
		return
	}
	template, err := h.service.Update(r.Context(), bc.BusinessID, id, contentTemplateInput(req))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contentTemplateResponse(*template))
}

func (h *ContentTemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	bc, id, ok := templateRequestScope(w, r, "DeleteContentTemplate", authz.PermContentDelete)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), bc.BusinessID, id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ContentTemplateHandler) Render(w http.ResponseWriter, r *http.Request) {
	bc, id, ok := templateRequestScope(w, r, "RenderContentTemplate", authz.PermContentRead)
	if !ok {
		return
	}
	req, ok := decodeAndValidate[openapi.RenderContentTemplateRequest](w, r, "invalid_template_values")
	if !ok {
		return
	}
	content, err := h.service.Render(r.Context(), bc.BusinessID, id, req.Values)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, openapi.RenderContentTemplateResponse{Content: content})
}

func (h *ContentTemplateHandler) CreateFromPost(w http.ResponseWriter, r *http.Request) {
	h.createFromSource(w, r, "CreateContentTemplateFromPost", chi.URLParam(r, "postId"), true)
}

func (h *ContentTemplateHandler) CreateFromReview(w http.ResponseWriter, r *http.Request) {
	h.createFromSource(w, r, "CreateContentTemplateFromReview", chi.URLParam(r, "reviewId"), false)
}

func (h *ContentTemplateHandler) createFromSource(w http.ResponseWriter, r *http.Request, op, sourceID string, post bool) {
	bc, ok := requireBusiness(w, r, op, authz.PermContentCreate)
	if !ok {
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	req, ok := decodeAndValidate[openapi.CreateContentTemplateFromSourceRequest](w, r, "invalid_template")
	if !ok {
		return
	}
	var template *domain.ContentTemplate
	var err error
	if post {
		template, err = h.service.CreateFromPost(r.Context(), bc.BusinessID, userID, sourceID, req.Name)
	} else {
		template, err = h.service.CreateFromReview(r.Context(), bc.BusinessID, userID, sourceID, req.Name)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/businesses/%s/content-templates/%s", bc.BusinessID, template.ID))
	writeJSON(w, http.StatusCreated, contentTemplateResponse(*template))
}

func templateRequestScope(w http.ResponseWriter, r *http.Request, op string, perm authz.Permission) (authz.BusinessContext, uuid.UUID, bool) {
	bc, ok := requireBusiness(w, r, op, perm)
	if !ok {
		return authz.BusinessContext{}, uuid.Nil, false
	}
	id, ok := parseUUIDParam(w, r, "templateId", "invalid_template_id")
	return bc, id, ok
}

func (h *ContentTemplateHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrContentTemplateNotFound), errors.Is(err, domain.ErrPostNotFound), errors.Is(err, domain.ErrReviewNotFound), errors.Is(err, domain.ErrBusinessNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, domain.ErrContentTemplateLimit):
		writeJSONError(w, http.StatusConflict, "template_limit_reached")
	case errors.Is(err, service.ErrInvalidContentTemplate):
		writeJSONError(w, http.StatusBadRequest, "invalid_template")
	default:
		slog.ErrorContext(r.Context(), "content template request failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}
