package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/service"
)

func TestResolveRejectsUnownedPhotoAsBadRequest(t *testing.T) {
	h := &HITLHandler{}
	response := httptest.NewRecorder()
	h.mapResolveError(response, httptest.NewRequest(http.MethodPost, "/resolve", http.NoBody), service.ErrHITLInvalidPhotoURL)
	require.Equal(t, http.StatusBadRequest, response.Code)
}
