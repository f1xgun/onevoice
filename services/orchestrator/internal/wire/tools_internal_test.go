package wire

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/imagegen"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// stubGenerator returns a canned Result/error and counts calls.
type stubGenerator struct {
	res   *imagegen.Result
	err   error
	calls int
}

func (s *stubGenerator) Generate(_ context.Context, _ imagegen.Request) (*imagegen.Result, error) {
	s.calls++
	return s.res, s.err
}
func (s *stubGenerator) Name() string { return "openai-image" }

// stubStore records uploads and returns a fixed public URL.
type stubStore struct {
	uploads int
	lastKey string
	url     string
}

func (s *stubStore) Upload(_ context.Context, key string, _ io.Reader, _ int64, _ string) error {
	s.uploads++
	s.lastKey = key
	return nil
}
func (s *stubStore) PublicURL(_ string) string { return s.url }

// stubWriter captures billing rows.
type stubWriter struct {
	logs []*llm.UsageLog
}

func (s *stubWriter) LogUsage(_ context.Context, log *llm.UsageLog) error {
	s.logs = append(s.logs, log)
	return nil
}

func defNames(defs []llm.ToolDefinition) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Function.Name
	}
	return out
}

func testCfg() *config.Config { return &config.Config{LLMTier: "free"} }

// TestRegisterInternalTools_WithGenerator is the fail-on-revert guard: a non-nil
// generator registers generate_image with a Manual floor (image generation
// spends money and is injection-steerable, so it pauses for HITL). It is still
// offered with no active integrations, but a Manual floor is NOT auto-exempt from
// an explicit project whitelist — the tool must be listed to be offered there.
func TestRegisterInternalTools_WithGenerator(t *testing.T) {
	reg := toolregistry.NewRegistry()
	RegisterInternalTools(reg, &stubGenerator{}, &stubStore{}, &stubWriter{}, testCfg())

	require.True(t, reg.Has(tools.GenerateImage), "generate_image must be registered")
	assert.Equal(t, domain.ToolFloorManual, reg.Floor(tools.GenerateImage))

	assert.Contains(t, defNames(reg.Available(nil)), tools.GenerateImage,
		"bare-name tool must be available with no active integrations")

	wlOmitted := reg.AvailableForWhitelist(context.Background(), nil, domain.WhitelistModeExplicit, nil)
	assert.NotContains(t, defNames(wlOmitted), tools.GenerateImage,
		"Manual-floor tool must NOT pass an EXPLICIT whitelist unless listed")

	wlListed := reg.AvailableForWhitelist(context.Background(), nil, domain.WhitelistModeExplicit, []string{tools.GenerateImage})
	assert.Contains(t, defNames(wlListed), tools.GenerateImage,
		"an explicitly whitelisted generate_image must be offered")
}

// TestRegisterInternalTools_NilGenerator proves a disabled generator adds no tool.
func TestRegisterInternalTools_NilGenerator(t *testing.T) {
	reg := toolregistry.NewRegistry()
	RegisterInternalTools(reg, nil, &stubStore{}, &stubWriter{}, testCfg())
	assert.False(t, reg.Has(tools.GenerateImage), "nil generator must not register generate_image")
}

// TestGenerateImageExecutor_WritesUsageLog checks the end-to-end executor: it
// uploads under a business-namespaced key, writes a non-zero cost row tagged
// openai-image, and returns the store's public photo_url.
func TestGenerateImageExecutor_WritesUsageLog(t *testing.T) {
	gen := &stubGenerator{res: &imagegen.Result{
		Data: []byte("\x89PNG bytes"), ContentType: "image/png",
		Width: 1024, Height: 1024, Model: "dall-e-3", CostUSD: 0.04,
	}}
	store := &stubStore{url: "https://cdn.example/media/generated/abc.png"}
	billing := &stubWriter{}
	exec := newGenerateImageExecutor(gen, store, billing, testCfg())

	bizID := uuid.New()
	ctx := a2a.WithBusinessID(context.Background(), bizID.String())
	out, err := exec(ctx, map[string]interface{}{"prompt": "a cat", "size": "1024x1024"})
	require.NoError(t, err)

	m, ok := out.(map[string]any)
	require.True(t, ok, "result must be a map so photo_url surfaces to the LLM")
	assert.Equal(t, store.url, m["photo_url"])
	assert.Equal(t, 1024, m["width"])

	assert.Equal(t, 1, gen.calls)
	assert.Equal(t, 1, store.uploads)
	assert.True(t, strings.HasPrefix(store.lastKey, "generated/"+bizID.String()+"/"),
		"object key must be namespaced by business id, got %q", store.lastKey)

	require.Len(t, billing.logs, 1)
	log := billing.logs[0]
	assert.Equal(t, "openai-image", log.Provider)
	assert.Equal(t, "dall-e-3", log.Model)
	assert.Equal(t, bizID, log.BusinessID)
	assert.Greater(t, log.ProviderCostUSD, 0.0, "billing row must record non-zero provider cost")
	assert.Greater(t, log.CommissionUSD, 0.0)
}

// TestGenerateImageExecutor_MissingBusinessID refuses to store an unattributed
// object (tenant isolation / 152-ФЗ erasability).
func TestGenerateImageExecutor_MissingBusinessID(t *testing.T) {
	store := &stubStore{}
	exec := newGenerateImageExecutor(&stubGenerator{}, store, &stubWriter{}, testCfg())
	_, err := exec(context.Background(), map[string]interface{}{"prompt": "x"})
	require.Error(t, err)
	assert.Equal(t, 0, store.uploads, "must not upload without a business id")
}

// TestGenerateImageExecutor_GenError_NoUpload proves an oversize/transient
// generator error short-circuits before Upload and yields a clean tool error.
func TestGenerateImageExecutor_GenError_NoUpload(t *testing.T) {
	gen := &stubGenerator{err: fmt.Errorf("%w: too big", imagegen.ErrResultTooLarge)}
	store := &stubStore{}
	exec := newGenerateImageExecutor(gen, store, &stubWriter{}, testCfg())

	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	_, err := exec(ctx, map[string]interface{}{"prompt": "x"})
	require.Error(t, err)
	assert.Equal(t, 0, store.uploads, "no upload when generation fails")
	assert.NotContains(t, err.Error(), "too big", "raw provider detail must not leak to the tool result")
}

// TestGenerateImageExecutor_UnsafePrompt maps a content-policy rejection to a
// clean, non-retry-inviting message.
func TestGenerateImageExecutor_UnsafePrompt(t *testing.T) {
	gen := &stubGenerator{err: fmt.Errorf("%w: policy", imagegen.ErrUnsafePrompt)}
	exec := newGenerateImageExecutor(gen, &stubStore{}, &stubWriter{}, testCfg())
	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	_, err := exec(ctx, map[string]interface{}{"prompt": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rephrase")
}

// okResult is a canned successful generation used by the guardrail tests below.
func okResult() *imagegen.Result {
	return &imagegen.Result{
		Data: []byte("\x89PNG bytes"), ContentType: "image/png",
		Width: 1024, Height: 1024, Model: "dall-e-3", CostUSD: 0.04,
	}
}

// TestGenerateImageExecutor_MalformedBusinessID rejects a non-UUID business id
// BEFORE any paid call — no generation, no upload, no billing write.
func TestGenerateImageExecutor_MalformedBusinessID(t *testing.T) {
	gen := &stubGenerator{res: okResult()}
	store := &stubStore{}
	billing := &stubWriter{}
	exec := newGenerateImageExecutor(gen, store, billing, testCfg())

	ctx := a2a.WithBusinessID(context.Background(), "not-a-uuid")
	_, err := exec(ctx, map[string]interface{}{"prompt": "a cat"})
	require.Error(t, err)
	assert.Equal(t, 0, gen.calls, "malformed business id must not reach the paid provider call")
	assert.Equal(t, 0, store.uploads)
	assert.Empty(t, billing.logs)
}

// TestGenerateImageExecutor_InvalidSize rejects an off-enum size BEFORE the paid
// call and returns a clean, allow-set-listing error.
func TestGenerateImageExecutor_InvalidSize(t *testing.T) {
	gen := &stubGenerator{res: okResult()}
	store := &stubStore{}
	exec := newGenerateImageExecutor(gen, store, &stubWriter{}, testCfg())

	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	_, err := exec(ctx, map[string]interface{}{"prompt": "a cat", "size": "512x512"})
	require.Error(t, err)
	assert.Equal(t, 0, gen.calls, "invalid size must not reach the paid provider call")
	assert.Equal(t, 0, store.uploads)
	assert.Contains(t, err.Error(), "1024x1024")
}

// TestGenerateImageExecutor_InvalidStyle rejects an off-enum style before the
// paid call.
func TestGenerateImageExecutor_InvalidStyle(t *testing.T) {
	gen := &stubGenerator{res: okResult()}
	exec := newGenerateImageExecutor(gen, &stubStore{}, &stubWriter{}, testCfg())

	ctx := a2a.WithBusinessID(context.Background(), uuid.New().String())
	_, err := exec(ctx, map[string]interface{}{"prompt": "a cat", "style": "photographic"})
	require.Error(t, err)
	assert.Equal(t, 0, gen.calls, "invalid style must not reach the paid provider call")
}

// TestGenerateImageExecutor_PerTurnCap proves a single turn cannot generate more
// than IMAGE_GEN_MAX_PER_TURN images: the over-cap call errors without a paid
// provider call or upload.
func TestGenerateImageExecutor_PerTurnCap(t *testing.T) {
	gen := &stubGenerator{res: okResult()}
	store := &stubStore{url: "https://cdn.example/media/x.png"}
	cfg := &config.Config{LLMTier: "free", ImageGenMaxPerTurn: 2}
	exec := newGenerateImageExecutor(gen, store, &stubWriter{}, cfg)

	ctx := imagegen.WithTurnBudget(a2a.WithBusinessID(context.Background(), uuid.New().String()))
	args := map[string]interface{}{"prompt": "a cat"}

	_, err1 := exec(ctx, args)
	require.NoError(t, err1)
	_, err2 := exec(ctx, args)
	require.NoError(t, err2)

	_, err3 := exec(ctx, args)
	require.Error(t, err3, "3rd image in one turn must be capped")
	assert.Contains(t, err3.Error(), "image limit")

	assert.Equal(t, 2, gen.calls, "over-cap call must not reach the paid provider call")
	assert.Equal(t, 2, store.uploads, "over-cap call must not upload")
}

// TestGenerateImageExecutor_StampsConversationID proves the image usage row
// carries the conversation id from context, matching the LLM usage row.
func TestGenerateImageExecutor_StampsConversationID(t *testing.T) {
	gen := &stubGenerator{res: okResult()}
	billing := &stubWriter{}
	exec := newGenerateImageExecutor(gen, &stubStore{url: "https://cdn.example/x.png"}, billing, testCfg())

	const convID = "67f4a8b27a9ad15d4f8a1c00"
	ctx := a2a.WithConversationID(a2a.WithBusinessID(context.Background(), uuid.New().String()), convID)
	_, err := exec(ctx, map[string]interface{}{"prompt": "a cat"})
	require.NoError(t, err)

	require.Len(t, billing.logs, 1)
	assert.Equal(t, convID, billing.logs[0].ConversationID)
}
