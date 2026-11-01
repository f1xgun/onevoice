package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

func TestReviewPolicyOptionalIgnoreFourStarPreservesLegacyWrites(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		stored     map[string]interface{}
		want       bool
	}{
		{"legacy fresh four-auto", `{"enabled":true,"minRating":4}`, nil, false},
		{"legacy preserves explicit ignore", `{"enabled":true,"minRating":4}`, map[string]interface{}{"ignoreFourStar": true}, true},
		{"new explicit false", `{"enabled":true,"minRating":5,"ignoreFourStar":false}`, map[string]interface{}{"ignoreFourStar": true}, false},
		{"new explicit true", `{"enabled":true,"minRating":5,"ignoreFourStar":true}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			businessID := uuid.New()
			svc := new(MockBusinessService)
			svc.On("GetByID", mock.Anything, businessID).Return(&domain.Business{ID: businessID, Settings: map[string]interface{}{platform.ReviewAutopilotSettingsKey: tc.stored}}, nil).Maybe()
			var saved platform.ReviewAutopilotConfig
			svc.On("UpdateSettingsKeys", mock.Anything, businessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
				saved = keys[platform.ReviewAutopilotSettingsKey].(platform.ReviewAutopilotConfig)
				return true
			}), mock.Anything).Return(&domain.Business{ID: businessID}, nil)
			h, err := NewBusinessHandler(svc, nil, nil)
			require.NoError(t, err)
			req := withBizCtx(httptest.NewRequest(http.MethodPut, "/review-autopilot", bytes.NewBufferString(tc.body)), bizPerms(businessID, uuid.New()))
			w := httptest.NewRecorder()
			h.UpdateReviewAutopilot(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.want, saved.IgnoreFourStar)
			require.True(t, saved.Enabled)
		})
	}
}

func TestReviewPolicyFactualConsentAndLegacyGlobalOff(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"explicit independent consent", `{"enabled":false,"minRating":5,"ignoreFourStar":true,"autoAnswerFacts":true}`, true},
		{"legacy global off", `{"enabled":false,"minRating":5}`, false},
		{"legacy preserves explicit while on", `{"enabled":true,"minRating":5}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			businessID := uuid.New()
			svc := new(MockBusinessService)
			svc.On("GetByID", mock.Anything, businessID).Return(&domain.Business{ID: businessID, Settings: map[string]interface{}{"reviewAutopilot": map[string]interface{}{"autoAnswerFacts": true}}}, nil).Maybe()
			var saved platform.ReviewAutopilotConfig
			svc.On("UpdateSettingsKeys", mock.Anything, businessID, mock.MatchedBy(func(keys map[string]interface{}) bool {
				saved = keys[platform.ReviewAutopilotSettingsKey].(platform.ReviewAutopilotConfig)
				return true
			}), mock.Anything).Return(&domain.Business{ID: businessID}, nil)
			h, err := NewBusinessHandler(svc, nil, nil)
			require.NoError(t, err)
			req := withBizCtx(httptest.NewRequest(http.MethodPut, "/review-autopilot", bytes.NewBufferString(tc.body)), bizPerms(businessID, uuid.New()))
			w := httptest.NewRecorder()
			h.UpdateReviewAutopilot(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.want, saved.AutoAnswerFacts)
		})
	}
}
