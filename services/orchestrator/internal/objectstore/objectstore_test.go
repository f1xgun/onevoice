package objectstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/safefetch"
)

// blockingEnsurer simulates a hung/unreachable object store: BucketExists blocks
// until the context deadline fires, then returns the context error — exactly the
// shape minio-go surfaces on a stalled round trip.
type blockingEnsurer struct{ existsCalls int }

func (b *blockingEnsurer) BucketExists(ctx context.Context, _ string) (bool, error) {
	b.existsCalls++
	<-ctx.Done()
	return false, ctx.Err()
}

func (b *blockingEnsurer) MakeBucket(_ context.Context, _ string, _ minio.MakeBucketOptions) error {
	return errors.New("MakeBucket must not be reached when BucketExists times out")
}

// TestEnsureBucket_TimesOut proves a hung object store cannot block boot: the
// bounded context fires and ensureBucket returns a deadline error quickly rather
// than hanging forever.
func TestEnsureBucket_TimesOut(t *testing.T) {
	be := &blockingEnsurer{}
	start := time.Now()
	err := ensureBucket(context.Background(), be, "onevoice", 50*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, be.existsCalls)
	assert.Less(t, elapsed, time.Second, "must return promptly on timeout, not hang")
}

// countingEnsurer records a bucket that already exists so MakeBucket is skipped.
type countingEnsurer struct {
	existsCalls int
	makeCalls   int
}

func (c *countingEnsurer) BucketExists(_ context.Context, _ string) (bool, error) {
	c.existsCalls++
	return true, nil
}

func (c *countingEnsurer) MakeBucket(_ context.Context, _ string, _ minio.MakeBucketOptions) error {
	c.makeCalls++
	return nil
}

// TestEnsureBucket_ExistingBucket_NoMake proves the happy path: an existing
// bucket is not re-created, and a zero timeout falls back to the default (no
// panic / no immediate deadline).
func TestEnsureBucket_ExistingBucket_NoMake(t *testing.T) {
	c := &countingEnsurer{}
	require.NoError(t, ensureBucket(context.Background(), c, "onevoice", 0))
	assert.Equal(t, 1, c.existsCalls)
	assert.Equal(t, 0, c.makeCalls, "existing bucket must not be recreated")
}

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

// TestNewMinioStore_RejectsNonHTTPSPublicURL locks in the https-only contract:
// an http:// or relative PublicURL boots fine but every produced photo_url is
// later rejected by safefetch.ValidateURL, so it must fail fast at construction.
// The scheme check runs before any S3 round-trip, so no MinIO server is needed.
func TestNewMinioStore_RejectsNonHTTPSPublicURL(t *testing.T) {
	cases := []struct {
		name      string
		publicURL string
	}{
		{"http scheme", "http://app.onevoice.example"},
		{"relative path", "/media"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMinioStore(context.Background(), Config{
				Endpoint:  "localhost:9000",
				Bucket:    "onevoice",
				PublicURL: tc.publicURL,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "PublicURL must be an absolute https:// URL")
		})
	}
}

// TestNewMinioStore_AcceptsHTTPSPublicURL confirms an https:// PublicURL passes
// scheme validation. A pre-canceled context short-circuits the BucketExists
// round-trip so no MinIO server is needed: the returned error must come from the
// canceled fetch, not from the scheme check.
func TestNewMinioStore_AcceptsHTTPSPublicURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewMinioStore(ctx, Config{
		Endpoint:  "localhost:9000",
		Bucket:    "onevoice",
		PublicURL: "https://app.onevoice.example",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "PublicURL must be an absolute https:// URL")
}
