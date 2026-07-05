package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// httpDoer is the minimal slice of *http.Client the generator depends on.
// Depending on this interface (not the concrete client) lets tests inject a
// fake that returns canned operation JSON, so no network call is made.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Yandex Cloud hosts. RE-VERIFY at enablement: the exact Foundation Models
// host, the async image-generation path, and the long-running-operation host
// are Yandex-documented values that may change; the feature ships OFF and is
// only wired once real keys are supplied, so these are validated then.
const (
	// yandexArtSubmitURL is the async text-to-image submission endpoint.
	yandexArtSubmitURL = "https://llm.api.cloud.yandex.net/foundationModels/v1/imageGenerationAsync"
	// yandexOperationBaseURL is the long-running-operation polling host; the
	// operation id is appended as "/operations/{id}".
	yandexOperationBaseURL = "https://operation.api.cloud.yandex.net"
)

const (
	// yandexProviderName is the Name()/billing label. It is intentionally
	// distinct from the chat provider so image spend is filterable in usage_logs.
	yandexProviderName = "yandexart"
	// yandexArtModel is the model id stamped on Result and the billing row.
	yandexArtModel = "yandex-art"
	// yandexContentType is the MIME type produced (JPEG per generationOptions).
	yandexContentType = "image/jpeg"
	// yandexMimeType is the generationOptions mimeType requested from the API.
	yandexMimeType = "image/jpeg"
)

// Per-image price, confirmed from the official Yandex AI Studio pricing page
// (2026-07-05): 2.23 ₽ incl. VAT per image-generation request. Requests are not
// idempotent — every generation is billed — so this is charged once per call.
// Kept non-zero so an image billing row is never $0 (which would make the
// daily-spend gate undercount).
const (
	// priceYandexARTImageRUB is the per-image price in RUB (official tariff).
	priceYandexARTImageRUB = 2.23
	// rubToUSD converts the RUB tariff to the USD unit the billing ledger
	// stores. Approximate fixed factor (the ledger is USD-denominated), not a
	// live FX rate; revisit if RUB/USD drifts materially.
	rubToUSD = 0.011
	// priceYandexARTImageUSD is the derived per-image list price stamped for
	// billing (≈$0.025 at the current factor).
	priceYandexARTImageUSD = priceYandexARTImageRUB * rubToUSD
)

// Poll cadence. The submit call returns an operation id; the image is fetched
// by polling the operation until done. The executor bounds the whole loop by
// ToolExecTimeout (~180s) via ctx, so these only shape the backoff.
const (
	defaultYandexPollInterval    = 1 * time.Second
	defaultYandexMaxPollInterval = 5 * time.Second
)

// yandexOpErrorBodyMax truncates a provider error body kept in the returned
// error string so a large response cannot bloat logs.
const yandexOpErrorBodyMax = 256

// pollResponseOverheadBytes is the JSON-envelope headroom added on top of the
// base64-expanded image cap when limiting a poll response read.
const pollResponseOverheadBytes = 64 << 10

// Landscape/portrait aspect-ratio numerators/denominators (1:1 uses the literal
// 1). The square side lengths reuse openai.go's sideShort/sideLong consts.
const (
	aspectSide16 = 16
	aspectSide9  = 9
)

// gRPC status codes surfaced in a done-operation error object. Only the
// transient set is retryable → ErrProvider; every other code is a client/
// content rejection → ErrUnsafePrompt.
const (
	grpcDeadlineExceeded  = 4
	grpcResourceExhausted = 8
	grpcAborted           = 10
	grpcInternal          = 13
	grpcUnavailable       = 14
	grpcDataLoss          = 15
)

// retryableYandexGRPCCodes is the set of operation-error codes worth retrying.
var retryableYandexGRPCCodes = map[int]struct{}{
	grpcDeadlineExceeded:  {},
	grpcResourceExhausted: {},
	grpcAborted:           {},
	grpcInternal:          {},
	grpcUnavailable:       {},
	grpcDataLoss:          {},
}

// yandexSizeSpec maps an allowed size string onto the aspect ratio sent to the
// API and the approximate pixel dimensions stamped on Result.
type yandexSizeSpec struct {
	aspectW, aspectH int
	pxW, pxH         int
}

// yandexSizeSpecs is the size→aspect table. Keys reuse the shared allow-set so
// the tool contract (JSON schema + ValidateParams) stays unchanged across
// providers: 1024x1024→1:1, 1792x1024→16:9, 1024x1792→9:16.
var yandexSizeSpecs = map[string]yandexSizeSpec{
	openai.CreateImageSize1024x1024: {aspectW: 1, aspectH: 1, pxW: sideShort, pxH: sideShort},
	openai.CreateImageSize1792x1024: {aspectW: aspectSide16, aspectH: aspectSide9, pxW: sideLong, pxH: sideShort},
	openai.CreateImageSize1024x1792: {aspectW: aspectSide9, aspectH: aspectSide16, pxW: sideShort, pxH: sideLong},
}

// yandexImageRateCard is the static per-image list price (USD) keyed by model.
// YandexART bills per image independent of size, so a single entry suffices;
// the fallback keeps a billing row from ever landing at $0.
var yandexImageRateCard = map[string]float64{
	yandexArtModel: priceYandexARTImageUSD,
}

// yandexPriceUSD returns the static per-image list price for the model, falling
// back to the placeholder price so a billing row is never $0.
func yandexPriceUSD(model string) float64 {
	if p, ok := yandexImageRateCard[model]; ok {
		return p
	}
	return priceYandexARTImageUSD
}

// YandexARTGenerator implements Generator against Yandex Cloud AI Studio
// Foundation Models (YandexART). Generation is asynchronous: submit returns an
// operation id, then the operation is polled until the base64 image is ready.
type YandexARTGenerator struct {
	doer            httpDoer
	apiKey          string
	folderID        string
	size            string
	maxBytes        int64
	pollInterval    time.Duration
	maxPollInterval time.Duration
}

// NewYandexARTGenerator builds a generator backed by a real *http.Client.
func NewYandexARTGenerator(cfg Config) *YandexARTGenerator {
	return newYandexARTGenerator(&http.Client{}, cfg)
}

// newYandexARTGenerator is the seam tests call with a fake httpDoer.
func newYandexARTGenerator(doer httpDoer, cfg Config) *YandexARTGenerator {
	size := mapSize(cfg.Size)
	if size == "" {
		size = openai.CreateImageSize1024x1024
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &YandexARTGenerator{
		doer:            doer,
		apiKey:          cfg.APIKey,
		folderID:        cfg.FolderID,
		size:            size,
		maxBytes:        maxBytes,
		pollInterval:    defaultYandexPollInterval,
		maxPollInterval: defaultYandexMaxPollInterval,
	}
}

// Name returns the billing/logging provider label.
func (g *YandexARTGenerator) Name() string { return yandexProviderName }

// yandexGenRequest is the imageGenerationAsync request body.
type yandexGenRequest struct {
	ModelURI          string                  `json:"modelUri"`
	Messages          []yandexMessage         `json:"messages"`
	GenerationOptions yandexGenerationOptions `json:"generationOptions"`
}

type yandexMessage struct {
	Text   string `json:"text"`
	Weight int    `json:"weight"`
}

type yandexGenerationOptions struct {
	MimeType    string            `json:"mimeType"`
	AspectRatio yandexAspectRatio `json:"aspectRatio"`
}

// yandexAspectRatio carries the ratio as strings: the underlying proto fields
// are int64, which Yandex's REST surface serializes/accepts as strings.
type yandexAspectRatio struct {
	WidthRatio  string `json:"widthRatio"`
	HeightRatio string `json:"heightRatio"`
}

// yandexOperation is the long-running-operation envelope returned by both the
// submit call and each poll.
type yandexOperation struct {
	ID       string            `json:"id"`
	Done     bool              `json:"done"`
	Response *yandexOpResponse `json:"response"`
	Error    *yandexOpError    `json:"error"`
}

type yandexOpResponse struct {
	Image        string `json:"image"`
	ModelVersion string `json:"modelVersion"`
}

type yandexOpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Generate submits the prompt, polls the operation to completion, decodes the
// base64 JPEG, enforces the byte cap, and stamps the static list price. A 4xx /
// content-policy rejection surfaces as ErrUnsafePrompt (not retryable); a
// 5xx/timeout/network error as ErrProvider. Style has no YandexART analog and
// is accepted-and-ignored so the tool contract stays stable across providers.
func (g *YandexARTGenerator) Generate(ctx context.Context, req Request) (*Result, error) {
	size := mapSize(req.Size)
	if size == "" {
		size = g.size
	}
	spec := aspectForSize(size)

	body := yandexGenRequest{
		ModelURI: g.modelURI(),
		Messages: []yandexMessage{{Text: req.Prompt, Weight: 1}},
		GenerationOptions: yandexGenerationOptions{
			MimeType: yandexMimeType,
			AspectRatio: yandexAspectRatio{
				WidthRatio:  strconv.Itoa(spec.aspectW),
				HeightRatio: strconv.Itoa(spec.aspectH),
			},
		},
	}

	image, err := g.run(ctx, body)
	if err != nil {
		return nil, err
	}

	data, decErr := base64.StdEncoding.DecodeString(image)
	if decErr != nil {
		return nil, fmt.Errorf("%w: decode image: %v", ErrProvider, decErr)
	}
	if int64(len(data)) > g.maxBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds cap %d", ErrResultTooLarge, len(data), g.maxBytes)
	}

	return &Result{
		Data:        data,
		ContentType: yandexContentType,
		Width:       spec.pxW,
		Height:      spec.pxH,
		Model:       yandexArtModel,
		CostUSD:     yandexPriceUSD(yandexArtModel),
	}, nil
}

// run submits the request and polls to completion, returning the base64 image.
func (g *YandexARTGenerator) run(ctx context.Context, body yandexGenRequest) (string, error) {
	op, err := g.submit(ctx, body)
	if err != nil {
		return "", err
	}
	if op.Done {
		return terminalImage(op)
	}
	if op.ID == "" {
		return "", fmt.Errorf("%w: submit returned no operation id", ErrProvider)
	}
	return g.poll(ctx, op.ID)
}

// modelURI builds the art:// model reference for the configured folder.
func (g *YandexARTGenerator) modelURI() string {
	return "art://" + g.folderID + "/yandex-art/latest"
}

// submit posts the generation request and returns the operation envelope.
func (g *YandexARTGenerator) submit(ctx context.Context, body yandexGenRequest) (*yandexOperation, error) {
	payload, mErr := json.Marshal(body)
	if mErr != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrProvider, mErr)
	}
	httpReq, rErr := http.NewRequestWithContext(ctx, http.MethodPost, yandexArtSubmitURL, bytes.NewReader(payload))
	if rErr != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrProvider, rErr)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Api-Key "+g.apiKey)
	return g.do(httpReq)
}

// poll fetches the operation until it is done, returning the base64 image. It
// checks ctx before each request and between polls so a hung generation cannot
// outlive the executor's ToolExecTimeout budget.
func (g *YandexARTGenerator) poll(ctx context.Context, opID string) (string, error) {
	url := yandexOperationBaseURL + "/operations/" + opID
	interval := g.pollInterval
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%w: image generation timed out: %v", ErrProvider, err)
		}
		httpReq, rErr := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if rErr != nil {
			return "", fmt.Errorf("%w: build poll request: %v", ErrProvider, rErr)
		}
		httpReq.Header.Set("Authorization", "Api-Key "+g.apiKey)

		op, err := g.do(httpReq)
		if err != nil {
			return "", err
		}
		if op.Done {
			return terminalImage(op)
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%w: image generation timed out: %v", ErrProvider, ctx.Err())
		case <-time.After(interval):
		}
		interval = nextInterval(interval, g.maxPollInterval)
	}
}

// do executes an HTTP request and decodes the operation envelope, mapping
// transport errors and non-2xx statuses onto package sentinels.
func (g *YandexARTGenerator) do(req *http.Request) (*yandexOperation, error) {
	resp, err := g.doer.Do(req)
	if err != nil {
		return nil, classifyYandexErr(err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, g.maxBytes*2+pollResponseOverheadBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, yandexStatusError(resp.StatusCode, string(raw))
	}
	var op yandexOperation
	if uErr := json.Unmarshal(raw, &op); uErr != nil {
		return nil, fmt.Errorf("%w: decode operation: %v", ErrProvider, uErr)
	}
	return &op, nil
}

// terminalImage extracts the base64 image from a done operation, mapping an
// operation-level error onto a sentinel.
func terminalImage(op *yandexOperation) (string, error) {
	if op.Error != nil {
		return "", classifyYandexOpError(op.Error.Code, op.Error.Message)
	}
	if op.Response == nil || op.Response.Image == "" {
		return "", fmt.Errorf("%w: operation done but returned no image", ErrProvider)
	}
	return op.Response.Image, nil
}

// aspectForSize returns the aspect/pixel spec for an allowed size, defaulting
// to 1:1 for an unexpected value (mapSize already normalizes the input).
func aspectForSize(size string) yandexSizeSpec {
	if s, ok := yandexSizeSpecs[size]; ok {
		return s
	}
	return yandexSizeSpecs[openai.CreateImageSize1024x1024]
}

// nextInterval grows the poll interval by ×1.5, capped at maxInterval.
func nextInterval(cur, maxInterval time.Duration) time.Duration {
	next := cur + cur/2
	if next > maxInterval {
		return maxInterval
	}
	return next
}

// classifyYandexErr maps a transport error (network, timeout, connection reset)
// onto ErrProvider — these are transient and a retry may succeed. It mirrors the
// no-status branch of classifyOpenAIErr.
func classifyYandexErr(err error) error {
	return fmt.Errorf("%w: %v", ErrProvider, err)
}

// yandexStatusError maps a non-2xx HTTP status onto a sentinel: a 4xx (content
// policy, bad request, auth) is non-retryable → ErrUnsafePrompt; a 5xx is
// transient → ErrProvider. Mirrors classifyOpenAIErr's status split.
func yandexStatusError(status int, body string) error {
	if status >= 400 && status < 500 {
		return fmt.Errorf("%w: yandexart http %d: %s", ErrUnsafePrompt, status, truncateBody(body))
	}
	return fmt.Errorf("%w: yandexart http %d: %s", ErrProvider, status, truncateBody(body))
}

// classifyYandexOpError maps a done-operation gRPC error code onto a sentinel.
// Retryable codes (deadline/exhausted/aborted/internal/unavailable/data-loss)
// → ErrProvider; every other code (invalid-argument, permission, content
// rejection) is non-retryable → ErrUnsafePrompt.
func classifyYandexOpError(code int, msg string) error {
	if _, retryable := retryableYandexGRPCCodes[code]; retryable {
		return fmt.Errorf("%w: yandexart operation failed (code %d): %s", ErrProvider, code, truncateBody(msg))
	}
	return fmt.Errorf("%w: yandexart operation rejected (code %d): %s", ErrUnsafePrompt, code, truncateBody(msg))
}

// truncateBody bounds a provider-supplied string kept in a returned error.
func truncateBody(s string) string {
	if len(s) > yandexOpErrorBodyMax {
		return s[:yandexOpErrorBodyMax]
	}
	return s
}
