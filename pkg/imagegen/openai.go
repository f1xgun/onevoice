package imagegen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// imageCreator is the minimal slice of *openai.Client the generator depends on.
// Depending on this interface (not the concrete client) lets tests inject a
// fake that returns canned B64JSON, so no network call is made.
type imageCreator interface {
	CreateImage(ctx context.Context, req openai.ImageRequest) (openai.ImageResponse, error)
}

// imageProviderName is the Provider label stamped on the billing row and
// returned by Name(). It is intentionally distinct from the chat "openai"
// provider so image spend is filterable in usage_logs.
const imageProviderName = "openai-image"

// imageContentType is the MIME type produced by the b64_json response format.
const imageContentType = "image/png"

// DALL·E 3 standard-quality per-image list prices (USD), as published by
// OpenAI. Square is the 1024×1024 price; the two 1024×1792 / 1792×1024 sizes
// share a higher price.
const (
	priceDallE3Square = 0.040
	priceDallE3Wide   = 0.080
)

// fallbackPriceUSD is stamped when a (model, size) pair is absent from the rate
// card, so a billing row is never $0 (which would make the daily-spend gate
// undercount image spend).
const fallbackPriceUSD = priceDallE3Square

// sideShort / sideLong are the two pixel edge lengths DALL·E 3 emits.
const (
	sideShort = 1024
	sideLong  = 1792
)

// imageRateCard is the static per-image list price (USD) keyed by
// "<model>|<size>". This is the authoritative cost source for the billing row —
// the orchestrator stamps Result.CostUSD from here, independent of the per-token
// LLM rate card.
var imageRateCard = map[string]float64{
	openai.CreateImageModelDallE3 + "|" + openai.CreateImageSize1024x1024: priceDallE3Square,
	openai.CreateImageModelDallE3 + "|" + openai.CreateImageSize1024x1792: priceDallE3Wide,
	openai.CreateImageModelDallE3 + "|" + openai.CreateImageSize1792x1024: priceDallE3Wide,
}

// OpenAIGenerator implements Generator against OpenAI's image API (DALL·E 3).
type OpenAIGenerator struct {
	client   imageCreator
	model    string
	size     string
	maxBytes int64
}

// NewOpenAIGenerator builds a generator backed by a real *openai.Client.
func NewOpenAIGenerator(cfg Config) *OpenAIGenerator {
	return newOpenAIGenerator(openai.NewClient(cfg.APIKey), cfg)
}

// newOpenAIGenerator is the seam tests call with a fake imageCreator.
func newOpenAIGenerator(client imageCreator, cfg Config) *OpenAIGenerator {
	model := cfg.Model
	if model == "" {
		model = openai.CreateImageModelDallE3
	}
	size := mapSize(cfg.Size)
	if size == "" {
		size = openai.CreateImageSize1024x1024
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &OpenAIGenerator{client: client, model: model, size: size, maxBytes: maxBytes}
}

// Name returns the billing/logging provider label.
func (g *OpenAIGenerator) Name() string { return imageProviderName }

// Generate calls the image API forcing b64_json (durable bytes, not OpenAI's
// ~1h ephemeral URL), decodes the first image, enforces the byte cap, and
// stamps the static list price. A 4xx is surfaced as ErrUnsafePrompt (not
// retryable); a 5xx/timeout/network error as ErrProvider.
func (g *OpenAIGenerator) Generate(ctx context.Context, req Request) (*Result, error) {
	size := mapSize(req.Size)
	if size == "" {
		size = g.size
	}
	imgReq := openai.ImageRequest{
		Prompt:         req.Prompt,
		Model:          g.model,
		N:              1,
		Size:           size,
		ResponseFormat: openai.CreateImageResponseFormatB64JSON,
	}
	if req.Style != "" {
		imgReq.Style = req.Style
	}

	resp, err := g.client.CreateImage(ctx, imgReq)
	if err != nil {
		return nil, classifyOpenAIErr(err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("%w: empty image response", ErrProvider)
	}

	b64 := resp.Data[0].B64JSON
	if b64 == "" {
		return nil, fmt.Errorf("%w: provider returned no b64_json (url-only is not durable)", ErrProvider)
	}
	data, decErr := base64.StdEncoding.DecodeString(b64)
	if decErr != nil {
		return nil, fmt.Errorf("%w: decode b64_json: %v", ErrProvider, decErr)
	}
	if int64(len(data)) > g.maxBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds cap %d", ErrResultTooLarge, len(data), g.maxBytes)
	}

	w, h := dimsForSize(size)
	return &Result{
		Data:        data,
		ContentType: imageContentType,
		Width:       w,
		Height:      h,
		Model:       g.model,
		CostUSD:     priceUSD(g.model, size),
		Revised:     resp.Data[0].RevisedPrompt,
	}, nil
}

// classifyOpenAIErr maps a go-openai transport error onto a package sentinel.
// A 4xx (content policy, bad request) is non-retryable → ErrUnsafePrompt; a
// 5xx / no-status (timeout, network) is transient → ErrProvider.
func classifyOpenAIErr(err error) error {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		if apiErr.HTTPStatusCode >= 400 && apiErr.HTTPStatusCode < 500 {
			return fmt.Errorf("%w: %v", ErrUnsafePrompt, err)
		}
		return fmt.Errorf("%w: %v", ErrProvider, err)
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		if reqErr.HTTPStatusCode >= 400 && reqErr.HTTPStatusCode < 500 {
			return fmt.Errorf("%w: %v", ErrUnsafePrompt, err)
		}
		return fmt.Errorf("%w: %v", ErrProvider, err)
	}
	return fmt.Errorf("%w: %v", ErrProvider, err)
}

// mapSize returns size when it is a DALL·E 3-supported value, else "".
func mapSize(size string) string {
	switch size {
	case openai.CreateImageSize1024x1024,
		openai.CreateImageSize1024x1792,
		openai.CreateImageSize1792x1024:
		return size
	default:
		return ""
	}
}

// dimsForSize returns the pixel width/height for a supported size string.
func dimsForSize(size string) (w, h int) {
	switch size {
	case openai.CreateImageSize1024x1792:
		return sideShort, sideLong
	case openai.CreateImageSize1792x1024:
		return sideLong, sideShort
	default:
		return sideShort, sideShort
	}
}

// priceUSD returns the static list price for a (model, size) pair, falling back
// to fallbackPriceUSD so a billing row is never $0.
func priceUSD(model, size string) float64 {
	if p, ok := imageRateCard[model+"|"+size]; ok {
		return p
	}
	return fallbackPriceUSD
}
