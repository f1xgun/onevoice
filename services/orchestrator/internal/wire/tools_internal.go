package wire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/imagegen"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/objectstore"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

const (
	// imageGenRuDescription / imageGenEnDescription are the LLM-facing tool
	// descriptions. They steer the model away from hallucinating a photo_url:
	// it must call generate_image first and thread the RETURNED photo_url into a
	// photo-publishing tool.
	imageGenRuDescription = "Генерирует изображение по текстовому описанию и возвращает публичный HTTPS-URL готовой картинки в поле photo_url. Используй этот инструмент, когда для поста нужна картинка, которой у пользователя ещё нет. НИКОГДА не придумывай photo_url сам — сначала вызови generate_image, затем передай полученный photo_url в telegram__send_channel_photo / vk__post_photo / yandex_business__upload_photo."
	imageGenEnDescription = "Generates an image from a text prompt and returns a public HTTPS URL of the finished picture in the photo_url field. Call this when a post needs an image the user has not supplied. NEVER invent a photo_url — call generate_image first, then pass the returned photo_url to telegram__send_channel_photo / vk__post_photo / yandex_business__upload_photo."

	// imageCommissionMode is the commission model used for the image usage row.
	imageCommissionMode = "tiered"

	// imageBillingTimeout bounds the synchronous billing POST so a hung sink
	// cannot pin the tool call open for the full ToolExecTimeout.
	imageBillingTimeout = 5 * time.Second
)

// generateImageSpec is the declarative tool definition for generate_image. Bare
// name (no "{platform}__" prefix) → always offered; Auto floor → no HITL gate
// (the downstream publish tool is still Manual-gated, and cost is handled by the
// daily-spend gate, not HITL).
func generateImageSpec() toolregistry.ToolSpec {
	return toolregistry.ToolSpec{
		DisplayName:    "Сгенерировать изображение",
		DisplayNameKey: "tools.generate_image.name",
		UserDescription: "Создаёт изображение по текстовому описанию и возвращает ссылку на готовую картинку " +
			"для публикации.",
		DescriptionEn: imageGenEnDescription,
		ParameterDescriptionsEn: map[string]string{
			"prompt": "Text description of the image to generate",
			"size":   "Image size",
			"style":  "Optional style hint",
		},
		Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
			Name:        tools.GenerateImage,
			Description: imageGenRuDescription,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Текстовое описание изображения",
					},
					"size": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"1024x1024", "1024x1792", "1792x1024"},
						"default":     "1024x1024",
						"description": "Размер изображения",
					},
					"style": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"vivid", "natural"},
						"description": "Стиль изображения (необязательно)",
					},
				},
				"required": []string{"prompt"},
			},
		}},
		Floor:          domain.ToolFloorAuto,
		EditableFields: nil,
	}
}

// RegisterInternalTools registers the orchestrator-local tools whose executors
// run in-process (no NATS). Currently just generate_image. A nil generator is a
// no-op, so a disabled/unconfigured image feature adds nothing to the registry.
func RegisterInternalTools(
	reg *toolregistry.Registry,
	gen imagegen.Generator,
	store objectstore.ObjectStore,
	billing llm.Writer,
	cfg *config.Config,
) {
	if gen == nil || store == nil {
		return
	}
	reg.Register(generateImageSpec(), newGenerateImageExecutor(gen, store, billing, cfg))
}

// newGenerateImageExecutor builds the in-process executor. Flow: read the
// business id from ctx (the same seam platform executors use) → generate bytes
// → upload to the object store under a business-namespaced key → record a
// non-zero usage row → return the absolute photo_url so the LLM threads it into
// a photo tool. It NEVER echoes a user-supplied URL back as generated.
func newGenerateImageExecutor(
	gen imagegen.Generator,
	store objectstore.ObjectStore,
	billing llm.Writer,
	cfg *config.Config,
) toolregistry.ExecutorFunc {
	return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		prompt := strings.TrimSpace(stringArg(args, "prompt"))
		if prompt == "" {
			return nil, fmt.Errorf("generate_image: prompt is required")
		}

		businessID := a2a.BusinessIDFromContext(ctx)
		if businessID == "" {
			return nil, fmt.Errorf("generate_image: missing business context")
		}

		res, err := gen.Generate(ctx, imagegen.Request{
			Prompt: prompt,
			Size:   stringArg(args, "size"),
			Style:  stringArg(args, "style"),
			N:      1,
		})
		if err != nil {
			return nil, mapGenError(err)
		}

		key := "generated/" + businessID + "/" + uuid.NewString() + ".png"
		if upErr := store.Upload(ctx, key, bytes.NewReader(res.Data), int64(len(res.Data)), res.ContentType); upErr != nil {
			slog.ErrorContext(ctx, "generate_image: upload failed", "error", upErr, "business_id", businessID)
			return nil, fmt.Errorf("generate_image: could not store the generated image, try again")
		}

		recordImageUsage(ctx, billing, cfg, gen.Name(), businessID, res)

		return map[string]any{
			"photo_url": store.PublicURL(key),
			"width":     res.Width,
			"height":    res.Height,
		}, nil
	}
}

// mapGenError converts an imagegen sentinel into a clean, user-safe tool-result
// error string. A 4xx (ErrUnsafePrompt) must NOT invite a retry loop; transient
// errors say "try again"; the raw provider detail stays in logs.
func mapGenError(err error) error {
	switch {
	case errors.Is(err, imagegen.ErrUnsafePrompt):
		return fmt.Errorf("generate_image: the prompt was rejected by the image content policy — rephrase it and do not retry the same wording")
	case errors.Is(err, imagegen.ErrResultTooLarge):
		return fmt.Errorf("generate_image: the generated image was too large; try a simpler prompt")
	case errors.Is(err, imagegen.ErrProvider):
		return fmt.Errorf("generate_image: image generation is temporarily unavailable, try again shortly")
	default:
		return fmt.Errorf("generate_image: could not generate the image")
	}
}

// recordImageUsage writes one usage_logs row for the image so its cost feeds the
// per-business daily-spend gate. Best-effort and bounded: a billing failure is
// logged, never surfaced to the user (the image already succeeded).
func recordImageUsage(
	ctx context.Context,
	billing llm.Writer,
	cfg *config.Config,
	provider, businessID string,
	res *imagegen.Result,
) {
	if billing == nil {
		return
	}
	bizUUID, err := uuid.Parse(businessID)
	if err != nil || bizUUID == uuid.Nil {
		slog.WarnContext(ctx, "generate_image: skipping billing, invalid business id", "business_id", businessID)
		return
	}
	tier := cfg.LLMTier
	commission := llm.CalculateCommission(res.CostUSD, imageCommissionMode, tier)

	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageBillingTimeout)
	defer cancel()
	if logErr := billing.LogUsage(bctx, &llm.UsageLog{
		ID:              uuid.New(),
		BusinessID:      bizUUID,
		Model:           res.Model,
		Provider:        provider,
		ProviderCostUSD: res.CostUSD,
		CommissionUSD:   commission,
		UserCostUSD:     res.CostUSD + commission,
		UserTier:        tier,
		CreatedAt:       time.Now(),
	}); logErr != nil {
		slog.WarnContext(ctx, "generate_image: billing write failed", "error", logErr, "business_id", businessID)
	}
}

// stringArg reads a string tool argument, tolerating a missing/non-string value.
func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// ImageGen builds the configured image generator, or nil when image generation
// is disabled/unconfigured. nil is the signal Tools uses to skip registration.
func ImageGen(cfg *config.Config) imagegen.Generator {
	return imagegen.New(imagegen.Config{
		Enabled:  cfg.ImageGenEnabled,
		Provider: cfg.ImageGenProvider,
		APIKey:   cfg.OpenAIAPIKey,
		Model:    cfg.ImageGenModel,
		Size:     cfg.ImageGenSize,
		MaxBytes: cfg.ImageGenMaxBytes,
	})
}

// ImageStore builds the orchestrator-local object store for generated media.
// Returns an error (not nil) so the caller can degrade the generate_image tool
// to unregistered rather than boot with a half-wired feature.
func ImageStore(ctx context.Context, cfg *config.Config) (objectstore.ObjectStore, error) {
	return objectstore.NewMinioStore(ctx, objectstore.Config{
		Endpoint:  cfg.S3Endpoint,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3Bucket,
		UseSSL:    cfg.S3UseSSL,
		PublicURL: cfg.PublicURL,
	})
}
