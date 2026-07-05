// Package imagegen turns a text prompt into image bytes via a pluggable
// provider. It is storage-agnostic on purpose: Generate returns the raw image
// bytes plus metadata, never a provider-hosted URL. The orchestrator persists
// those bytes to its own object store and hands the LLM a durable HTTPS URL, so
// the existing photo-publishing tools can post an image the platform agents can
// actually download.
package imagegen

import (
	"context"
	"errors"
)

// Sentinel errors. Callers use errors.Is to map a failure to a user-facing
// tool-result string and to decide whether a retry is worthwhile.
var (
	// ErrDisabled is returned by a generator that is present but switched off.
	ErrDisabled = errors.New("imagegen: disabled")

	// ErrUnsafePrompt marks a provider content-policy / bad-request rejection
	// (HTTP 4xx). It is NOT retryable — the same prompt will fail again.
	ErrUnsafePrompt = errors.New("imagegen: prompt rejected by provider")

	// ErrProvider wraps a transient provider failure (HTTP 5xx, timeout,
	// network error, malformed response). A retry may succeed.
	ErrProvider = errors.New("imagegen: provider error")

	// ErrResultTooLarge is returned when the decoded image exceeds the
	// configured byte cap, before any upload is attempted.
	ErrResultTooLarge = errors.New("imagegen: generated image too large")
)

// DefaultMaxBytes caps a decoded image. It matches safefetch.DefaultMaxBytes
// (10 MiB) so a generated object can never exceed the ceiling the platform
// agents' SSRF-safe fetcher enforces when they later download it to re-upload.
const DefaultMaxBytes int64 = 10 << 20

// Request is a storage-agnostic image-generation request.
type Request struct {
	// Prompt is the text description of the desired image (required).
	Prompt string
	// Size is one of the provider's supported "WxH" strings; "" uses the
	// generator's configured default.
	Size string
	// Style is an optional provider-specific style hint (e.g. "vivid").
	Style string
	// N is the number of images to request; the orchestrator only ever uses 1.
	N int
}

// Result carries the generated image bytes and their metadata.
type Result struct {
	// Data is the decoded image (never a URL).
	Data []byte
	// ContentType is the MIME type of Data (e.g. "image/png").
	ContentType string
	// Width and Height are the pixel dimensions.
	Width, Height int
	// Model is the provider model id that produced the image.
	Model string
	// CostUSD is the static list price stamped for billing.
	CostUSD float64
	// Revised is the provider's rewritten prompt, when supplied.
	Revised string
}

// Generator produces an image from a text prompt.
type Generator interface {
	// Generate returns the image bytes for req, or one of the package sentinels.
	Generate(ctx context.Context, req Request) (*Result, error)
	// Name identifies the generator for billing/logging (e.g. "openai-image").
	Name() string
}

// Config configures the generator factory.
type Config struct {
	// Enabled gates construction; when false New returns nil.
	Enabled bool
	// Provider selects the backend ("openai" (default when "") or "yandexart").
	Provider string
	// APIKey is the provider credential; when empty New returns nil. For
	// yandexart this is the Yandex Cloud "Api-Key".
	APIKey string
	// FolderID is the Yandex Cloud folder id, the second yandexart credential;
	// it forms the art:// model uri. Unused by the openai provider.
	FolderID string
	// Model overrides the provider default model id.
	Model string
	// Size is the default image size when a request omits it.
	Size string
	// MaxBytes caps the decoded image; <= 0 uses DefaultMaxBytes.
	MaxBytes int64
}

// New returns the generator selected by cfg, or nil when image generation is
// disabled or unconfigured (Enabled false, empty APIKey, or unknown provider).
// A nil Generator is the signal every call site uses to skip registration, so
// the feature stays completely inert unless explicitly turned on.
func New(cfg Config) Generator {
	if !cfg.Enabled || cfg.APIKey == "" {
		return nil
	}
	switch cfg.Provider {
	case "", "openai":
		return NewOpenAIGenerator(cfg)
	case "yandexart":
		return NewYandexARTGenerator(cfg)
	default:
		return nil
	}
}
