package storage

import "testing"

// newTestClient builds a MinioClient with only the URL-prefix wiring populated.
// The minio *client is nil — these tests exercise the pure URL helpers only.
func newTestClient(prefix string) *MinioClient {
	return &MinioClient{publicURLPrefix: prefix}
}

// TestKeyFromPublicURL_RoundTrips verifies KeyFromPublicURL inverts PublicURL so
// the UploadLogo replace path can recover the prior object key from a stored
// LogoURL and delete it.
func TestKeyFromPublicURL_RoundTrips(t *testing.T) {
	m := newTestClient("/media")
	const key = "businesses/abc/logo-123.png"

	url := m.PublicURL(key)
	got := m.KeyFromPublicURL(url)

	if got != key {
		t.Fatalf("KeyFromPublicURL(%q) = %q, want %q", url, got, key)
	}
}

// TestKeyFromPublicURL_ForeignURLReturnsEmpty verifies a URL that does not carry
// this store's prefix yields "" so the caller skips it instead of deleting an
// unrelated key.
func TestKeyFromPublicURL_ForeignURLReturnsEmpty(t *testing.T) {
	m := newTestClient("/media")

	for _, url := range []string{
		"",
		"https://cdn.example.com/logo.png",
		"/other/businesses/abc/logo.png",
	} {
		if got := m.KeyFromPublicURL(url); got != "" {
			t.Errorf("KeyFromPublicURL(%q) = %q, want empty", url, got)
		}
	}
}
