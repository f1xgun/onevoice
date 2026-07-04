// Package objectstore uploads generated media to the orchestrator's own
// MinIO/S3 bucket and returns an ABSOLUTE public HTTPS URL for it.
//
// It deliberately mirrors services/api/internal/storage but is a separate,
// orchestrator-local type: the two services are distinct Go modules and the
// orchestrator needs one behavioral difference — PublicURL returns an absolute
// https URL (scheme + host), not the relative "/media/..." the api emits. The
// generated URL is handed straight to the LLM and then to a photo-publishing
// tool, whose agent downloads it through safefetch.ValidateURL; a relative URL
// has an empty host and would be rejected.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore uploads blobs and returns an absolute public URL for them.
type ObjectStore interface {
	// Upload writes reader (size bytes) to key with the given content type.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	// PublicURL returns the absolute https URL a client (or agent) can fetch key from.
	PublicURL(key string) string
}

// mediaPathPrefix is the URL path segment under which uploaded media is served
// (nginx in prod, a dev rewrite locally) — matches services/api's "/media".
const mediaPathPrefix = "/media/"

// Config holds MinIO/S3 connection settings plus the absolute public base URL.
type Config struct {
	// Endpoint is the S3 host:port (no scheme).
	Endpoint string
	// AccessKey / SecretKey are the S3 credentials.
	AccessKey string
	SecretKey string
	// Bucket is the target bucket (e.g. "onevoice").
	Bucket string
	// UseSSL toggles TLS to the S3 endpoint.
	UseSSL bool
	// PublicURL is the absolute public origin (e.g. "https://app.example.com")
	// that generated media URLs are rooted at. MUST be absolute so the produced
	// photo_url passes safefetch.ValidateURL.
	PublicURL string
}

// MinioStore implements ObjectStore against a MinIO / S3-compatible server.
type MinioStore struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

// NewMinioStore creates a MinIO-backed ObjectStore and ensures the bucket
// exists. It requires a non-empty absolute PublicURL so it can never emit a
// host-less URL that safefetch would reject downstream.
func NewMinioStore(ctx context.Context, cfg Config) (*MinioStore, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("objectstore: endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("objectstore: bucket is required")
	}
	publicURL := strings.TrimRight(cfg.PublicURL, "/")
	if !strings.HasPrefix(publicURL, "https://") && !strings.HasPrefix(publicURL, "http://") {
		return nil, fmt.Errorf("objectstore: PublicURL must be an absolute http(s) URL, got %q", cfg.PublicURL)
	}

	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("objectstore: new minio client: %w", err)
	}

	exists, err := cli.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("objectstore: check bucket exists: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("objectstore: create bucket %q: %w", cfg.Bucket, err)
		}
	}

	return &MinioStore{client: cli, bucket: cfg.Bucket, publicURL: publicURL}, nil
}

// Upload streams reader to the configured bucket under key.
func (m *MinioStore) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("objectstore: put object %q: %w", key, err)
	}
	return nil
}

// PublicURL returns an absolute https URL: publicURL + "/media/" + key.
func (m *MinioStore) PublicURL(key string) string {
	return m.publicURL + mediaPathPrefix + strings.TrimLeft(key, "/")
}
