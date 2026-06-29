// Package storage provides object storage abstractions for user uploads.
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Uploader uploads blobs to an object store under a given key.
type Uploader interface {
	// Upload writes reader (size bytes) to key with the given content type.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	// PublicURL returns the URL the frontend should use to fetch key.
	PublicURL(key string) string
	// KeyFromPublicURL recovers the object key from a PublicURL value, or "" when
	// the URL does not carry this store's public prefix.
	KeyFromPublicURL(url string) string
	// Delete removes the object stored under key. Removing a missing key is a
	// no-op (not an error).
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every object whose key starts with prefix. Used to
	// reclaim a tenant's objects on hard-delete / 152-ФЗ erasure.
	DeletePrefix(ctx context.Context, prefix string) error
}

// Config holds MinIO/S3 connection settings.
type Config struct {
	Endpoint        string // host:port, no scheme
	AccessKey       string
	SecretKey       string
	Bucket          string
	UseSSL          bool
	PublicURLPrefix string // e.g. "/media" — prepended to object key for client-facing URLs
}

// MinioClient implements Uploader against a MinIO / S3-compatible server.
type MinioClient struct {
	client          *minio.Client
	bucket          string
	publicURLPrefix string
}

// NewMinioClient creates a MinIO-backed Uploader and ensures the bucket exists.
func NewMinioClient(ctx context.Context, cfg Config) (*MinioClient, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage: endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: bucket is required")
	}
	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: new minio client: %w", err)
	}

	exists, err := cli.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: check bucket exists: %w", err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: create bucket %q: %w", cfg.Bucket, err)
		}
	}

	return &MinioClient{
		client:          cli,
		bucket:          cfg.Bucket,
		publicURLPrefix: strings.TrimRight(cfg.PublicURLPrefix, "/"),
	}, nil
}

// Upload streams reader to the configured bucket under key.
func (m *MinioClient) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", key, err)
	}
	return nil
}

// PublicURL returns publicURLPrefix/key — a stable URL the frontend can embed.
// The object is served either by nginx (prod) or a dev rewrite routing /media/* to MinIO.
func (m *MinioClient) PublicURL(key string) string {
	return m.publicURLPrefix + "/" + strings.TrimLeft(key, "/")
}

// KeyFromPublicURL recovers the object key from a value previously produced by
// PublicURL by stripping the configured public prefix. It returns "" when url
// does not carry the expected prefix, so callers can skip a stale/foreign URL
// instead of deleting an unrelated key.
func (m *MinioClient) KeyFromPublicURL(url string) string {
	want := m.publicURLPrefix + "/"
	if !strings.HasPrefix(url, want) {
		return ""
	}
	return strings.TrimPrefix(url, want)
}

// Delete removes the object stored under key. A missing key is treated as a
// successful no-op by the server.
func (m *MinioClient) Delete(ctx context.Context, key string) error {
	if err := m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: remove object %q: %w", key, err)
	}
	return nil
}

// DeletePrefix removes every object whose key starts with prefix by listing the
// prefix recursively and removing each returned object.
func (m *MinioClient) DeletePrefix(ctx context.Context, prefix string) error {
	objectsCh := m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for removeErr := range m.client.RemoveObjects(ctx, m.bucket, objectsCh, minio.RemoveObjectsOptions{}) {
		if removeErr.Err != nil {
			return fmt.Errorf("storage: remove objects under %q: %w", prefix, removeErr.Err)
		}
	}
	return nil
}
