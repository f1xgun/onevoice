package i18n_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/i18n"
)

// TestLocaleMiddleware_InjectsResolvedTagIntoContext mirrors the table the
// api + orchestrator middleware tests used before consolidation. Keeping the
// cases here means the deletion of those two stub tests does not shrink
// coverage — every Accept-Language variant is still exercised end-to-end
// against the canonical shared middleware.
func TestLocaleMiddleware_InjectsResolvedTagIntoContext(t *testing.T) {
	tests := []struct {
		name           string
		acceptLanguage string
		want           language.Tag
	}{
		{name: "english header", acceptLanguage: "en", want: language.English},
		{name: "russian header", acceptLanguage: "ru-RU", want: language.Russian},
		{name: "empty header falls back to default", acceptLanguage: "", want: i18n.DefaultTag},
		{name: "unsupported header falls back to default", acceptLanguage: "fr", want: i18n.DefaultTag},
		{name: "garbage header falls back to default", acceptLanguage: "not-a-valid-lang-tag!!!", want: i18n.DefaultTag},
		{name: "weighted prefers higher q", acceptLanguage: "en-US;q=0.9, ru;q=0.5", want: language.English},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured language.Tag

			handler := i18n.LocaleMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = i18n.LocaleFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if tt.acceptLanguage != "" {
				req.Header.Set("Accept-Language", tt.acceptLanguage)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			wantBase, _ := tt.want.Base()
			gotBase, _ := captured.Base()
			assert.Equal(t, wantBase.String(), gotBase.String(),
				"Accept-Language=%q want=%s got=%s", tt.acceptLanguage, tt.want, captured)
		})
	}
}

// TestLocaleMiddleware_PassesContextThrough — defensive check that the
// middleware actually rewires the request ctx (via r.WithContext) instead of
// silently dropping the tag. LocaleFromContext on a bare ctx returns
// DefaultTag (Russian), so observing language.English proves the new ctx
// reached the wrapped handler.
func TestLocaleMiddleware_PassesContextThrough(t *testing.T) {
	handler := i18n.LocaleMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := i18n.LocaleFromContext(r.Context())
		assert.Equal(t, language.English, got)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Accept-Language", "en")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
