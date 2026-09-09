package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recapReaderFake struct {
	got   uuid.UUID
	value *domain.WeeklyValueRecap
}

func (f *recapReaderFake) Latest(_ context.Context, id uuid.UUID, _ time.Time) (*domain.WeeklyValueRecap, error) {
	f.got = id
	return f.value, nil
}
func recapReadContext(id uuid.UUID, allowed bool) context.Context {
	p := []authz.Permission{}
	if allowed {
		p = []authz.Permission{authz.PermContentRead}
	}
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: id, UserID: uuid.New(), Permissions: p})
}
func TestWeeklyValueRecapHandlerTenantScopeAndEmpty(t *testing.T) {
	id := uuid.New()
	reader := &recapReaderFake{value: &domain.WeeklyValueRecap{PublishedPosts: 2}}
	h := NewWeeklyValueRecapHandler(reader)
	req := httptest.NewRequest(http.MethodGet, "/recap/latest", http.NoBody).WithContext(recapReadContext(id, true))
	rr := httptest.NewRecorder()
	h.GetLatest(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, id, reader.got)
	var got domain.WeeklyValueRecap
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.Equal(t, 2, got.PublishedPosts)
	reader.value = nil
	rr = httptest.NewRecorder()
	h.GetLatest(rr, req)
	assert.Equal(t, http.StatusNoContent, rr.Code)
}
func TestWeeklyValueRecapHandlerRequiresContentRead(t *testing.T) {
	reader := &recapReaderFake{}
	h := NewWeeklyValueRecapHandler(reader)
	req := httptest.NewRequest(http.MethodGet, "/recap/latest", http.NoBody).WithContext(recapReadContext(uuid.New(), false))
	rr := httptest.NewRecorder()
	h.GetLatest(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Equal(t, uuid.Nil, reader.got)
}
