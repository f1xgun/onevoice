package vk

import (
	"context"
	"net/http"
	"time"
)

// FetcherFunc adapts a function to the image-fetcher used by PostPhoto so tests
// can drive the upload path against a loopback server without tripping the SSRF
// guard. It is test-only.
type FetcherFunc func(ctx context.Context, rawURL string) ([]byte, string, error)

// Get satisfies the internal imageFetcher interface.
func (f FetcherFunc) Get(ctx context.Context, rawURL string) (body []byte, contentType string, err error) {
	return f(ctx, rawURL)
}

// SetPhotoFetcher swaps the package photo fetcher and returns a restore
// function. Test-only.
func SetPhotoFetcher(f FetcherFunc) func() {
	prev := photoFetcher
	photoFetcher = f
	return func() { photoFetcher = prev }
}

// HTTPTimeout exposes the timeout configured on the underlying VK SDK HTTP
// client so tests can assert REST calls are bounded. Test-only.
func (c *Client) HTTPTimeout() time.Duration {
	return c.vk.Client.Timeout
}

// SetHTTPTimeout overrides the underlying VK SDK HTTP client timeout so a test
// can inject a short bound and exercise the stalled-peer path without waiting
// the full production timeout. Test-only.
func (c *Client) SetHTTPTimeout(d time.Duration) {
	c.vk.Client = &http.Client{Timeout: d}
}
