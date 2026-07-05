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
	"github.com/f1xgun/onevoice/services/orchestrator/internal/reviewstats"
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

const (
	// reviewStatsRuDescription / reviewStatsEnDescription are the LLM-facing tool
	// descriptions. The tool answers the owner's reputation questions from their
	// OWN records and returns AGGREGATES ONLY — counts, averages, rates — never
	// review text or author names. It is scoped to the current organization; the
	// model does not (and cannot) choose whose stats to read.
	reviewStatsRuDescription = "Возвращает сводную статистику по отзывам текущей организации: сколько всего отзывов, сколько без ответа, доля отвеченных, средний рейтинг, распределение по звёздам, активность за последнюю неделю и медианное время ответа в часах. Используй этот инструмент, когда пользователь спрашивает о своей репутации в цифрах (например «сколько отзывов без ответа?», «какой средний рейтинг?», «за сколько часов я обычно отвечаю?», «сколько отзывов я закрыл за неделю?»). Инструмент возвращает только числа, без текста отзывов и имён авторов."
	reviewStatsEnDescription = "Returns aggregate statistics about the current organization's reviews: total count, unanswered count, reply rate, average rating, per-star distribution, recent-week activity, and median response time in hours. Call this when the user asks about their reputation in numbers (for example \"how many reviews are unanswered?\", \"what is my average rating?\", \"how fast do I usually reply?\", \"how many reviews did I close this week?\"). It returns numbers only — never review text or author names."

	// reviewStatsMinDays / reviewStatsMaxDays bound the recent-period window the
	// model may request, so a nonsensical value cannot silently widen the query.
	// reviewStatsDefaultDays is the advertised default ("за неделю").
	reviewStatsMinDays     = 1
	reviewStatsMaxDays     = 365
	reviewStatsDefaultDays = 7
)

// reviewStatsSpec is the declarative tool definition for get_review_stats. Bare
// name (no "{platform}__" prefix) → always offered; Auto floor → no HITL gate
// (a read that returns only aggregates has no side effect to approve).
func reviewStatsSpec() toolregistry.ToolSpec {
	return toolregistry.ToolSpec{
		DisplayName:    "Статистика по отзывам",
		DisplayNameKey: "tools.get_review_stats.name",
		UserDescription: "Считает сводные показатели по отзывам организации: всего, без ответа, доля ответов, " +
			"средний рейтинг, распределение по звёздам и активность за неделю.",
		DescriptionEn: reviewStatsEnDescription,
		ParameterDescriptionsEn: map[string]string{
			"recent_days": "Length in days of the recent-period window (default 7)",
		},
		Def: llm.ToolDefinition{Type: "function", Function: llm.FunctionDefinition{
			Name:        tools.GetReviewStats,
			Description: reviewStatsRuDescription,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"recent_days": map[string]interface{}{
						"type":        "integer",
						"minimum":     reviewStatsMinDays,
						"maximum":     reviewStatsMaxDays,
						"default":     reviewStatsDefaultDays,
						"description": "Длина окна недавнего периода в днях (по умолчанию 7)",
					},
				},
			},
		}},
		Floor:          domain.ToolFloorAuto,
		EditableFields: nil,
	}
}

// RegisterReviewStatsTool registers the read-only get_review_stats tool. A nil
// fetcher is a no-op, so an orchestrator without a review data source (e.g. no
// Mongo) simply does not offer the tool rather than registering a handler that
// would fail every call.
func RegisterReviewStatsTool(reg *toolregistry.Registry, fetcher reviewstats.Fetcher) {
	if fetcher == nil {
		return
	}
	reg.Register(reviewStatsSpec(), newReviewStatsExecutor(fetcher))
}

// newReviewStatsExecutor builds the in-process executor. The business id is read
// from trusted turn context (the same seam the image executor uses), NEVER from
// an LLM argument, so the model cannot spoof another organization's id to read
// its stats. The executor fetches the current business's reviews and returns
// aggregates only.
func newReviewStatsExecutor(fetcher reviewstats.Fetcher) toolregistry.ExecutorFunc {
	return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		businessID := a2a.BusinessIDFromContext(ctx)
		if businessID == "" {
			return nil, fmt.Errorf("get_review_stats: missing business context")
		}

		recentDays := clampRecentDays(intArg(args, "recent_days"))

		reviews, err := fetcher.FetchForBusiness(ctx, businessID)
		if err != nil {
			slog.ErrorContext(ctx, "get_review_stats: fetch failed", "error", err, "business_id", businessID)
			return nil, fmt.Errorf("get_review_stats: could not load review statistics, try again")
		}

		return reviewstats.Aggregate(reviews, time.Now(), recentDays), nil
	}
}

// clampRecentDays keeps the recent-period window within the advertised bounds.
// A zero (unset) value returns 0 so Aggregate applies its own default.
func clampRecentDays(days int) int {
	switch {
	case days == 0:
		return 0
	case days < reviewStatsMinDays:
		return reviewStatsMinDays
	case days > reviewStatsMaxDays:
		return reviewStatsMaxDays
	default:
		return days
	}
}

// intArg reads an integer tool argument, tolerating the JSON float64 the LLM
// transport delivers and a missing/non-numeric value.
func intArg(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
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
		bizUUID, idErr := uuid.Parse(businessID)
		if idErr != nil || bizUUID == uuid.Nil {
			return nil, fmt.Errorf("generate_image: missing or invalid business context")
		}

		size := stringArg(args, "size")
		style := stringArg(args, "style")
		if vErr := imagegen.ValidateParams(size, style); vErr != nil {
			return nil, mapGenError(vErr)
		}

		if !imagegen.ReserveTurnSlot(ctx, cfg.ImageGenMaxPerTurn) {
			return nil, fmt.Errorf(
				"generate_image: image limit for this message reached (max %d); do not generate more images in this turn",
				cfg.ImageGenMaxPerTurn)
		}

		res, err := gen.Generate(ctx, imagegen.Request{
			Prompt: prompt,
			Size:   size,
			Style:  style,
			N:      1,
		})
		if err != nil {
			return nil, mapGenError(err)
		}

		key := "generated/" + businessID + "/" + uuid.NewString() + extForContentType(res.ContentType)
		if upErr := store.Upload(ctx, key, bytes.NewReader(res.Data), int64(len(res.Data)), res.ContentType); upErr != nil {
			slog.ErrorContext(ctx, "generate_image: upload failed", "error", upErr, "business_id", businessID)
			return nil, fmt.Errorf("generate_image: could not store the generated image, try again")
		}

		recordImageUsage(ctx, billing, cfg, gen.Name(), bizUUID, res)

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
	case errors.Is(err, imagegen.ErrInvalidParam):
		return fmt.Errorf("generate_image: unsupported parameter — size must be one of 1024x1024, 1792x1024, 1024x1792 and style one of vivid, natural")
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
// per-business daily-spend gate. bizUUID is the already-validated business id (the
// executor rejects a malformed one before any paid call). The conversation id is
// read from ctx and stamped for attribution, matching how the LLM usage row is
// populated. Best-effort and bounded: a billing failure is logged, never
// surfaced to the user (the image already succeeded).
func recordImageUsage(
	ctx context.Context,
	billing llm.Writer,
	cfg *config.Config,
	provider string,
	bizUUID uuid.UUID,
	res *imagegen.Result,
) {
	if billing == nil {
		return
	}
	tier := cfg.LLMTier
	commission := llm.CalculateCommission(res.CostUSD, imageCommissionMode, tier)

	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageBillingTimeout)
	defer cancel()
	if logErr := billing.LogUsage(bctx, &llm.UsageLog{
		ID:              uuid.New(),
		BusinessID:      bizUUID,
		ConversationID:  a2a.ConversationIDFromContext(ctx),
		Model:           res.Model,
		Provider:        provider,
		ProviderCostUSD: res.CostUSD,
		CommissionUSD:   commission,
		UserCostUSD:     res.CostUSD + commission,
		UserTier:        tier,
		CreatedAt:       time.Now(),
	}); logErr != nil {
		slog.WarnContext(ctx, "generate_image: billing write failed", "error", logErr, "business_id", bizUUID)
	}
}

// stringArg reads a string tool argument, tolerating a missing/non-string value.
func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// extForContentType maps a generated image's MIME type onto the object-key
// suffix so a JPEG result is stored as .jpg and a PNG as .png. An unknown type
// falls back to .png (the historical default).
func extForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

// ImageGen builds the configured image generator, or nil when image generation
// is disabled/unconfigured. nil is the signal Tools uses to skip registration.
// The credential passed depends on the selected provider: OpenAI uses
// OPENAI_API_KEY; YandexART uses YANDEX_ART_API_KEY + YANDEX_ART_FOLDER_ID. The
// nil-when-disabled off-switch lives in imagegen.New (Enabled && APIKey != "").
func ImageGen(cfg *config.Config) imagegen.Generator {
	icfg := imagegen.Config{
		Enabled:  cfg.ImageGenEnabled,
		Provider: cfg.ImageGenProvider,
		Model:    cfg.ImageGenModel,
		Size:     cfg.ImageGenSize,
		MaxBytes: cfg.ImageGenMaxBytes,
	}
	switch cfg.ImageGenProvider {
	case "yandexart":
		icfg.APIKey = cfg.YandexArtAPIKey
		icfg.FolderID = cfg.YandexArtFolderID
	default:
		icfg.APIKey = cfg.OpenAIAPIKey
	}
	return imagegen.New(icfg)
}

// ImageStore builds the orchestrator-local object store for generated media.
// Returns an error (not nil) so the caller can degrade the generate_image tool
// to unregistered rather than boot with a half-wired feature.
func ImageStore(ctx context.Context, cfg *config.Config) (objectstore.ObjectStore, error) {
	return objectstore.NewMinioStore(ctx, objectstore.Config{
		Endpoint:      cfg.S3Endpoint,
		AccessKey:     cfg.S3AccessKey,
		SecretKey:     cfg.S3SecretKey,
		Bucket:        cfg.S3Bucket,
		UseSSL:        cfg.S3UseSSL,
		PublicURL:     cfg.PublicURL,
		BucketTimeout: cfg.ImageGenBucketTimeout,
	})
}
