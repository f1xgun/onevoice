package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type handlerTemplateRepo struct{ template domain.ContentTemplate }

func (r *handlerTemplateRepo) ListByBusinessID(context.Context, uuid.UUID) ([]domain.ContentTemplate, error) {
	return nil, nil
}
func (r *handlerTemplateRepo) GetByID(_ context.Context, businessID, id uuid.UUID) (*domain.ContentTemplate, error) {
	if r.template.BusinessID != businessID || r.template.ID != id {
		return nil, domain.ErrContentTemplateNotFound
	}
	cloned := r.template
	return &cloned, nil
}
func (r *handlerTemplateRepo) Create(context.Context, *domain.ContentTemplate, int) error { return nil }
func (r *handlerTemplateRepo) Update(context.Context, *domain.ContentTemplate) error      { return nil }
func (r *handlerTemplateRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error         { return nil }

func templateHandlerRequest(method, path string, businessID uuid.UUID, permissions ...authz.Permission) *http.Request {
	req := httptest.NewRequest(method, path, http.NoBody)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("templateId", path[strings.LastIndex(path, "/")+1:])
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	ctx = authz.WithBusinessContext(ctx, authz.BusinessContext{BusinessID: businessID, UserID: uuid.New(), Permissions: permissions})
	return req.WithContext(ctx)
}

func TestContentTemplateGetDoesNotCrossBusinessBoundary(t *testing.T) {
	template := domain.ContentTemplate{ID: uuid.New(), BusinessID: uuid.New(), CreatedBy: uuid.New(), Name: "A", Kind: domain.ContentTemplateKindPost, Body: "Body", Placeholders: []string{}}
	h, err := NewContentTemplateHandler(service.NewContentTemplateService(&handlerTemplateRepo{template: template}, nil, nil))
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	h.Get(rr, templateHandlerRequest(http.MethodGet, "/content-templates/"+template.ID.String(), uuid.New(), authz.PermContentRead))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestCreateFromSourceRequiresReadAndCreate(t *testing.T) {
	h, err := NewContentTemplateHandler(service.NewContentTemplateService(&handlerTemplateRepo{}, nil, nil))
	require.NoError(t, err)
	for _, permissions := range [][]authz.Permission{{authz.PermContentRead}, {authz.PermContentCreate}} {
		req := httptest.NewRequest(http.MethodPost, "/content-templates/from-post/post-1", strings.NewReader(`{"name":"A"}`))
		req = req.WithContext(authz.WithBusinessContext(req.Context(), authz.BusinessContext{BusinessID: uuid.New(), UserID: uuid.New(), Permissions: permissions}))
		rr := httptest.NewRecorder()
		h.CreateFromPost(rr, req)
		require.Equal(t, http.StatusForbidden, rr.Code)
	}
}
