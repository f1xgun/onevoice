package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImageCreator returns a canned response/error and records the last request.
type fakeImageCreator struct {
	resp   openai.ImageResponse
	err    error
	calls  int
	lastIn openai.ImageRequest
}

func (f *fakeImageCreator) CreateImage(_ context.Context, req openai.ImageRequest) (openai.ImageResponse, error) {
	f.calls++
	f.lastIn = req
	return f.resp, f.err
}

// b64Of encodes raw bytes as an OpenAI b64_json payload.
func b64Of(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func newGen(t *testing.T, client imageCreator, maxBytes int64) *OpenAIGenerator {
	t.Helper()
	return newOpenAIGenerator(client, Config{Model: openai.CreateImageModelDallE3, MaxBytes: maxBytes})
}

func TestGenerate_Success_DecodesBytesAndStampsCost(t *testing.T) {
	raw := []byte("\x89PNG\r\n fake image bytes")
	fake := &fakeImageCreator{resp: openai.ImageResponse{
		Data: []openai.ImageResponseDataInner{{B64JSON: b64Of(raw), RevisedPrompt: "a cat, refined"}},
	}}
	gen := newGen(t, fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "a cat", Size: "1792x1024", Style: "vivid"})
	require.NoError(t, err)
	assert.Equal(t, raw, res.Data)
	assert.Equal(t, "image/png", res.ContentType)
	assert.Equal(t, 1792, res.Width)
	assert.Equal(t, 1024, res.Height)
	assert.Equal(t, openai.CreateImageModelDallE3, res.Model)
	assert.InDelta(t, 0.080, res.CostUSD, 1e-9, "cost must be the 1792x1024 list price")
	assert.NotZero(t, res.CostUSD, "billing row must never be $0")
	assert.Equal(t, "a cat, refined", res.Revised)

	assert.Equal(t, openai.CreateImageResponseFormatB64JSON, fake.lastIn.ResponseFormat,
		"b64_json is mandatory — a URL-only response is not durable")
	assert.Equal(t, 1, fake.lastIn.N)
	assert.Equal(t, "1792x1024", fake.lastIn.Size)
	assert.Equal(t, "vivid", fake.lastIn.Style)
}

// TestGenerate_Oversize_ReturnsErrResultTooLarge is the guard that a decoded
// image larger than the cap is rejected BEFORE any upload — the executor never
// sees a Result, so Upload is never called.
func TestGenerate_Oversize_ReturnsErrResultTooLarge(t *testing.T) {
	big := bytes.Repeat([]byte{0x41}, 64)
	fake := &fakeImageCreator{resp: openai.ImageResponse{
		Data: []openai.ImageResponseDataInner{{B64JSON: b64Of(big)}},
	}}
	gen := newGen(t, fake, 16) // cap below the decoded size

	res, err := gen.Generate(context.Background(), Request{Prompt: "big"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResultTooLarge)
	assert.Nil(t, res)
}

// TestGenerate_B64Mandatory_URLOnlyIsProviderError proves a URL-only response
// (no b64_json) is rejected rather than silently returning a foreign URL.
func TestGenerate_B64Mandatory_URLOnlyIsProviderError(t *testing.T) {
	fake := &fakeImageCreator{resp: openai.ImageResponse{
		Data: []openai.ImageResponseDataInner{{URL: "https://oaidalle.example/ephemeral.png"}},
	}}
	gen := newGen(t, fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestGenerate_ContentPolicy4xx_IsUnsafePrompt(t *testing.T) {
	fake := &fakeImageCreator{err: &openai.APIError{
		HTTPStatusCode: 400,
		Message:        "Your request was rejected as a result of our safety system.",
	}}
	gen := newGen(t, fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "disallowed"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsafePrompt)
	assert.NotErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestGenerate_ServerError5xx_IsProvider(t *testing.T) {
	fake := &fakeImageCreator{err: &openai.APIError{HTTPStatusCode: 503, Message: "overloaded"}}
	gen := newGen(t, fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestGenerate_EmptyData_IsProviderError(t *testing.T) {
	fake := &fakeImageCreator{resp: openai.ImageResponse{Data: nil}}
	gen := newGen(t, fake, DefaultMaxBytes)

	_, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	assert.ErrorIs(t, err, ErrProvider)
}

func TestGenerate_UnknownSize_FallsBackToDefault(t *testing.T) {
	raw := []byte("img")
	fake := &fakeImageCreator{resp: openai.ImageResponse{
		Data: []openai.ImageResponseDataInner{{B64JSON: b64Of(raw)}},
	}}
	gen := newGen(t, fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x", Size: "999x999"})
	require.NoError(t, err)
	assert.Equal(t, "1024x1024", fake.lastIn.Size, "unknown size must fall back to the configured default")
	assert.Equal(t, 1024, res.Width)
	assert.Equal(t, 1024, res.Height)
}

func TestNew_DisabledOrUnconfigured_ReturnsNil(t *testing.T) {
	assert.Nil(t, New(Config{Enabled: false, APIKey: "sk-x"}), "disabled must yield nil")
	assert.Nil(t, New(Config{Enabled: true, APIKey: ""}), "empty api key must yield nil")
	assert.Nil(t, New(Config{Enabled: true, APIKey: "sk-x", Provider: "midjourney"}), "unknown provider must yield nil")
}

func TestNew_Enabled_ReturnsGenerator(t *testing.T) {
	gen := New(Config{Enabled: true, APIKey: "sk-x", Provider: "openai"})
	require.NotNil(t, gen)
	assert.Equal(t, "openai-image", gen.Name())
}
