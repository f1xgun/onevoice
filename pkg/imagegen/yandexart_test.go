package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeYandexDoer serves canned responses keyed by whether the request is the
// async submit (POST) or an operation poll (GET), records the submit body, and
// counts calls. No network is ever touched.
type fakeYandexDoer struct {
	submitStatus int
	submitOp     yandexOperation
	submitErr    error

	pollStatus int
	pollOps    []yandexOperation // consumed in order; last repeats
	pollErr    error

	submitCalls int
	pollCalls   int
	lastSubmit  yandexGenRequest
}

func (f *fakeYandexDoer) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost {
		f.submitCalls++
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &f.lastSubmit)
		}
		if f.submitErr != nil {
			return nil, f.submitErr
		}
		return jsonResponse(f.submitStatus, f.submitOp), nil
	}

	f.pollCalls++
	if f.pollErr != nil {
		return nil, f.pollErr
	}
	idx := f.pollCalls - 1
	if idx >= len(f.pollOps) {
		idx = len(f.pollOps) - 1
	}
	return jsonResponse(f.pollStatus, f.pollOps[idx]), nil
}

func jsonResponse(status int, op yandexOperation) *http.Response {
	if status == 0 {
		status = http.StatusOK
	}
	body, _ := json.Marshal(op)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

// rawResponseDoer returns a fixed status + raw body regardless of method, for
// the HTTP-status error-mapping tests.
type rawResponseDoer struct {
	status int
	body   string
	err    error
}

func (d *rawResponseDoer) Do(_ *http.Request) (*http.Response, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{
		StatusCode: d.status,
		Body:       io.NopCloser(strings.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}

func newYandexGen(doer httpDoer, maxBytes int64) *YandexARTGenerator {
	g := newYandexARTGenerator(doer, Config{APIKey: "AQVN-key", FolderID: "b1gfolder", MaxBytes: maxBytes})
	g.pollInterval = time.Millisecond
	g.maxPollInterval = time.Millisecond
	return g
}

func b64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func TestYandexGenerate_Success_SubmitPollDecode(t *testing.T) {
	raw := []byte("\xff\xd8\xff fake jpeg bytes")
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op-123", Done: false},
		pollOps: []yandexOperation{
			{ID: "op-123", Done: false},
			{ID: "op-123", Done: true, Response: &yandexOpResponse{Image: b64(raw), ModelVersion: "v1"}},
		},
	}
	gen := newYandexGen(fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "рыжий кот", Size: "1792x1024", Style: "vivid"})
	require.NoError(t, err)
	assert.Equal(t, raw, res.Data)
	assert.Equal(t, "image/jpeg", res.ContentType)
	assert.Equal(t, "yandex-art", res.Model)
	assert.Equal(t, 1792, res.Width)
	assert.Equal(t, 1024, res.Height)
	assert.NotZero(t, res.CostUSD, "billing row must never be $0")
	assert.InDelta(t, priceYandexARTImageUSD, res.CostUSD, 1e-9)

	assert.Equal(t, 1, fake.submitCalls)
	assert.Equal(t, 2, fake.pollCalls, "polls until done:true")
	assert.Equal(t, "art://b1gfolder/yandex-art/latest", fake.lastSubmit.ModelURI)
	require.Len(t, fake.lastSubmit.Messages, 1)
	assert.Equal(t, "рыжий кот", fake.lastSubmit.Messages[0].Text)
	assert.Equal(t, 1, fake.lastSubmit.Messages[0].Weight)
	assert.Equal(t, "image/jpeg", fake.lastSubmit.GenerationOptions.MimeType)
	assert.Equal(t, "16", fake.lastSubmit.GenerationOptions.AspectRatio.WidthRatio)
	assert.Equal(t, "9", fake.lastSubmit.GenerationOptions.AspectRatio.HeightRatio)
}

func TestYandexGenerate_SizeToAspectRatio(t *testing.T) {
	cases := []struct {
		size         string
		wantW, wantH string
		wantPxW      int
		wantPxH      int
	}{
		{"1024x1024", "1", "1", 1024, 1024},
		{"1792x1024", "16", "9", 1792, 1024},
		{"1024x1792", "9", "16", 1024, 1792},
	}
	for _, tc := range cases {
		t.Run(tc.size, func(t *testing.T) {
			raw := []byte("img")
			fake := &fakeYandexDoer{
				submitOp: yandexOperation{ID: "op", Done: false},
				pollOps: []yandexOperation{
					{ID: "op", Done: true, Response: &yandexOpResponse{Image: b64(raw)}},
				},
			}
			gen := newYandexGen(fake, DefaultMaxBytes)

			res, err := gen.Generate(context.Background(), Request{Prompt: "x", Size: tc.size})
			require.NoError(t, err)
			assert.Equal(t, tc.wantW, fake.lastSubmit.GenerationOptions.AspectRatio.WidthRatio)
			assert.Equal(t, tc.wantH, fake.lastSubmit.GenerationOptions.AspectRatio.HeightRatio)
			assert.Equal(t, tc.wantPxW, res.Width)
			assert.Equal(t, tc.wantPxH, res.Height)
		})
	}
}

func TestYandexGenerate_Oversize_ReturnsErrResultTooLarge(t *testing.T) {
	big := bytes.Repeat([]byte{0x41}, 64)
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op", Done: false},
		pollOps: []yandexOperation{
			{ID: "op", Done: true, Response: &yandexOpResponse{Image: b64(big)}},
		},
	}
	gen := newYandexGen(fake, 16) // cap below decoded size

	res, err := gen.Generate(context.Background(), Request{Prompt: "big"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResultTooLarge)
	assert.Nil(t, res)
}

func TestYandexGenerate_HTTP4xx_IsUnsafePrompt(t *testing.T) {
	doer := &rawResponseDoer{status: http.StatusBadRequest, body: `{"message":"content policy"}`}
	gen := newYandexGen(doer, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "disallowed"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsafePrompt)
	assert.NotErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestYandexGenerate_HTTP5xx_IsProvider(t *testing.T) {
	doer := &rawResponseDoer{status: http.StatusServiceUnavailable, body: "overloaded"}
	gen := newYandexGen(doer, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestYandexGenerate_TransportError_IsProvider(t *testing.T) {
	doer := &rawResponseDoer{err: context.DeadlineExceeded}
	gen := newYandexGen(doer, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestYandexGenerate_OperationError_ContentRejection_IsUnsafePrompt(t *testing.T) {
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op", Done: false},
		pollOps: []yandexOperation{
			{ID: "op", Done: true, Error: &yandexOpError{Code: 3, Message: "invalid argument"}},
		},
	}
	gen := newYandexGen(fake, DefaultMaxBytes)

	_, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsafePrompt)
}

func TestYandexGenerate_OperationError_Transient_IsProvider(t *testing.T) {
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op", Done: false},
		pollOps: []yandexOperation{
			{ID: "op", Done: true, Error: &yandexOpError{Code: 14, Message: "unavailable"}},
		},
	}
	gen := newYandexGen(fake, DefaultMaxBytes)

	_, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
}

// TestYandexGenerate_PollTimeout_RespectsCtx proves a never-completing operation
// is bounded by the context deadline and surfaces ErrProvider rather than
// spinning forever.
func TestYandexGenerate_PollTimeout_RespectsCtx(t *testing.T) {
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op", Done: false},
		pollOps:  []yandexOperation{{ID: "op", Done: false}}, // never done
	}
	gen := newYandexGen(fake, DefaultMaxBytes)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	res, err := gen.Generate(ctx, Request{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProvider)
	assert.Nil(t, res)
}

func TestYandexGenerate_InlineDoneOnSubmit_NoPoll(t *testing.T) {
	raw := []byte("inline")
	fake := &fakeYandexDoer{
		submitOp: yandexOperation{ID: "op", Done: true, Response: &yandexOpResponse{Image: b64(raw)}},
	}
	gen := newYandexGen(fake, DefaultMaxBytes)

	res, err := gen.Generate(context.Background(), Request{Prompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, raw, res.Data)
	assert.Equal(t, 0, fake.pollCalls, "a done submit must not trigger a poll")
}

func TestNewYandexART_DisabledOrNoKey_ReturnsNil(t *testing.T) {
	assert.Nil(t, New(Config{Enabled: false, APIKey: "k", Provider: "yandexart"}), "disabled must yield nil")
	assert.Nil(t, New(Config{Enabled: true, APIKey: "", Provider: "yandexart"}), "empty api key must yield nil")
}

func TestNewYandexART_Enabled_ReturnsGenerator(t *testing.T) {
	gen := New(Config{Enabled: true, APIKey: "k", FolderID: "f", Provider: "yandexart"})
	require.NotNil(t, gen)
	assert.Equal(t, "yandexart", gen.Name())
}
