package yandex

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newPNGServer starts an httptest server that returns a tiny PNG byte slice
// for any request. Used by RPA-method tests that need a downloadable image
// URL (UploadPhoto). Caller MUST defer srv.Close().
func newPNGServer(t *testing.T) *httptest.Server {
	t.Helper()
	// Smallest valid PNG: 8-byte signature + IHDR + IDAT + IEND.
	// We don't need it to actually decode — Yandex production reads bytes
	// and SetInputFiles ships the path. Any non-empty body suffices for
	// the test path.
	const png = "\x89PNG\r\n\x1a\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(png))
	}))
}
