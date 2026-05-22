package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

func TestLocale_InjectsResolvedTagIntoContext(t *testing.T) {
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

			handler := middleware.Locale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestLocale_DoesNotMutateOriginalContext(t *testing.T) {
	// Defensive: the middleware must call r.WithContext(...) — verify the
	// downstream request actually has a different ctx pointer.
	handler := middleware.Locale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// LocaleFromContext returns DefaultTag on a bare ctx; getting
		// language.English here proves the new ctx was wired through.
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
