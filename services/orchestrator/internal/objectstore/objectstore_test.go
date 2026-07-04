package objectstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/safefetch"
)

// TestPublicURL_IsAbsoluteAndSafefetchValid is the load-bearing guard: the URL
// handed back to the LLM must be an absolute https URL that the platform agents'
// SSRF-safe fetcher accepts. A relative "/media/..." (as services/api emits)
// would fail safefetch.ValidateURL with "empty host".
func TestPublicURL_IsAbsoluteAndSafefetchValid(t *testing.T) {
	m := &MinioStore{publicURL: "https://app.onevoice.example", bucket: "onevoice"}

	url := m.PublicURL("generated/biz-123/abc.png")
	assert.Equal(t, "https://app.onevoice.example/media/generated/biz-123/abc.png", url)

	require.NoError(t, safefetch.ValidateURL(url),
		"generated photo_url must pass the agents' SSRF validation")
}

func TestPublicURL_TrimsLeadingSlashOnKey(t *testing.T) {
	m := &MinioStore{publicURL: "https://cdn.example"}
	assert.Equal(t, "https://cdn.example/media/x/y.png", m.PublicURL("/x/y.png"))
}
