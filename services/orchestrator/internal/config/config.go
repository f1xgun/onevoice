package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/natsauth"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// defaultShutdownTimeout is the fallback graceful-shutdown budget when SHUTDOWN_TIMEOUT is unset.
const defaultShutdownTimeout = 30 * time.Second

// defaultMaxConcurrentStreams caps simultaneous SSE chat/resume streams a single
// orchestrator process will serve. It is a generous overload backstop, not a
// tuned capacity limit — the api already bounds per-user concurrency, so this
// only sheds load under a pathological aggregate burst. Set MAX_CONCURRENT_STREAMS
// to tune, or to <= 0 to disable.
const defaultMaxConcurrentStreams = 256

// defaultToolExecTimeout bounds a single tool call when TOOL_EXEC_TIMEOUT is
// unset. A platform agent that hangs (stuck RPA page, unanswered NATS request)
// must not pin an agent-loop iteration open indefinitely, so an empty env still
// gets a finite per-call deadline. The floor is set above the verified Yandex
// RPA path, whose own internal waits (pool-slot acquire + page nav + hydrate)
// plus retry backoff legitimately sum past a minute — a tighter default would
// time out calls that previously succeeded.
const defaultToolExecTimeout = 180 * time.Second

// defaultAPIInternalURL is the in-cluster mTLS endpoint dialed for internal API calls.
const defaultAPIInternalURL = "https://api:8443"

// defaultImageGenMaxBytes caps a generated image at 10 MiB — matching
// safefetch.DefaultMaxBytes so a persisted object can never exceed the ceiling
// the platform agents enforce when they later download it to re-upload.
const defaultImageGenMaxBytes = 10 * 1024 * 1024

// defaultImageGenMaxPerTurn caps how many images one chat turn may generate.
// Kept deliberately small: a single post rarely needs more than one or two
// images, and each generation is a paid provider call, so this bounds the
// worst-case per-turn image spend even if the model emits a burst of
// generate_image calls. Set IMAGE_GEN_MAX_PER_TURN to 0 to disable the cap.
const defaultImageGenMaxPerTurn = 2

// defaultImageGenBucketTimeout bounds the boot-time MinIO bucket
// existence/creation round trip so an unreachable or hung object store cannot
// stall orchestrator startup indefinitely. Only consulted when image
// generation is enabled — a disabled feature never touches the object store.
const defaultImageGenBucketTimeout = 10 * time.Second

// Cost-guard defaults; see docs/orchestrator/config.md and docs/llm-cost-guards.md.
const (
	// defaultConversationInputCap stops the agent loop once the running input
	// budget for a single conversation hits this many tokens.
	defaultConversationInputCap = 50000

	// defaultConversationOutputCap is the parallel limit on output tokens.
	defaultConversationOutputCap = 10000

	// defaultLocalFallbackRequestsPerHour roughly matches the "$10/h" ceiling
	// at the v1.4 average per-request cost (~$0.005).
	defaultLocalFallbackRequestsPerHour = 2000

	// defaultLocalFallbackWindow is how long the limiter consults the
	// in-process bucket before re-probing Redis.
	defaultLocalFallbackWindow = 30 * time.Second

	// redisDownPolicyBlock / redisDownPolicyLocalFallback are the literal
	// values accepted by LLM_RATELIMIT_ON_REDIS_DOWN.
	redisDownPolicyBlock         = "block"
	redisDownPolicyLocalFallback = "local_fallback"
)

// Config holds orchestrator configuration loaded from environment.
// Field reference table: docs/orchestrator/config.md.
type Config struct {
	Port            string
	LLMModel        string
	DraftReplyModel string
	LLMTier         string
	MaxIterations   int
	// MaxConcurrentStreams is the process-wide cap on simultaneous SSE chat +
	// resume streams. <= 0 disables the cap. Defaults to defaultMaxConcurrentStreams.
	MaxConcurrentStreams int
	NATSUrl              string
	ShutdownTimeout      time.Duration
	// ToolExecTimeout bounds a single tool call. Defaults to
	// defaultToolExecTimeout when TOOL_EXEC_TIMEOUT is unset so an empty env
	// still gets a finite per-call deadline.
	ToolExecTimeout time.Duration

	// HealthCheckTimeout caps any single dep ping inside /health/ready
	// (checks run concurrently → budget is max, not sum).
	HealthCheckTimeout time.Duration

	// Orchestrator owns a direct Mongo connection (avoids orchestrator→API→orchestrator cycle).
	MongoURI string
	MongoDB  string

	// LLM provider API keys (at least one must be set for the Router to boot).
	OpenRouterAPIKey string
	OpenAIAPIKey     string
	AnthropicAPIKey  string

	// APIInternalURL must be HTTPS — the mTLS substrate requires it on this endpoint.
	APIInternalURL string

	// InternalSecret is the shared service-to-service secret the orchestrator
	// requires on its cluster-internal inbound (chat / resume / tool registry /
	// draft-reply). Empty disables the guard for dev/tests; RequireInternalSecret
	// makes a non-empty, sufficiently long value mandatory in production.
	InternalSecret string

	SelfHostedEndpoints []llm.SelfHostedEndpoint

	// RedisURL empty disables rate-limiter wiring at boot.
	RedisURL string

	// FreeTierDailySpendUSD: 0 keeps compiled default, -1 disables gate, positive sets cap.
	FreeTierDailySpendUSD float64

	// ConversationInputCap / ConversationOutputCap: 0 disables that axis.
	ConversationInputCap  int
	ConversationOutputCap int

	// RedisDownPolicy is "block" (fail-closed) or "local_fallback".
	RedisDownPolicy string

	// LocalFallbackRequestsPerHour: required (>0) when policy is local_fallback.
	LocalFallbackRequestsPerHour int

	// LocalFallbackWindow is the re-probe interval after a Redis failure.
	LocalFallbackWindow time.Duration

	// AllowTransborderLLM, when true, disables outbound personal-data redaction
	// before LLM calls. Leave false (default) unless there is a legal basis for
	// transborder personal-data transfer or inference is routed only to
	// RU/self-hosted endpoints. See docs/orchestrator/config.md.
	AllowTransborderLLM bool

	// EnableGoogleBusiness gates registration of the Google Business tool set.
	// Google Business is not an MVP platform (the agent is unverified and the
	// platform is hidden on the integrations UI), so its tools are off by
	// default — otherwise they surface in Settings → Tools as approvable even
	// though the platform can never be connected.
	EnableGoogleBusiness bool

	// Image generation (generate_image tool). OFF by default: the tool is only
	// registered when ImageGenEnabled is true AND the selected provider's
	// credential plus an S3 endpoint are configured. See docs/orchestrator/config.md.
	ImageGenEnabled  bool
	ImageGenProvider string
	ImageGenModel    string
	ImageGenSize     string
	ImageGenMaxBytes int64

	// YandexArtAPIKey / YandexArtFolderID are the YandexART (Yandex Cloud AI
	// Studio) credentials, consulted only when ImageGenProvider is "yandexart".
	// Empty by default so the feature stays off until keys are supplied.
	YandexArtAPIKey   string
	YandexArtFolderID string

	// ImageGenMaxPerTurn caps images generated within a single agent turn.
	// <= 0 disables the cap. Defaults to defaultImageGenMaxPerTurn.
	ImageGenMaxPerTurn int

	// ImageGenBucketTimeout bounds the boot-time bucket existence/creation call
	// so a hung object store cannot block startup. Defaults to
	// defaultImageGenBucketTimeout. Only used when image generation is enabled.
	ImageGenBucketTimeout time.Duration

	// PublicURL is the absolute public origin (e.g. https://app.example.com)
	// that generated-media URLs are rooted at. MUST be absolute so a generated
	// photo_url passes the platform agents' safefetch validation.
	PublicURL string

	// S3* configure the orchestrator-local object store for generated media.
	// Mirror the api service's keys so one bucket serves both.
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3UseSSL    bool
}

// Load reads config from environment, applying defaults and failing loud on
// semantically invalid combinations. See docs/orchestrator/config.md.
func Load() (*Config, error) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		return nil, fmt.Errorf("LLM_MODEL is required")
	}

	maxIter := 10
	if v := os.Getenv("MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxIter = n
		}
	}

	maxStreams := defaultMaxConcurrentStreams
	if v := os.Getenv("MAX_CONCURRENT_STREAMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxStreams = n
		}
	}

	shutdownTimeout := defaultShutdownTimeout
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			shutdownTimeout = d
		}
	}

	toolExecTimeout := defaultToolExecTimeout
	if v := os.Getenv("TOOL_EXEC_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			toolExecTimeout = d
		}
	}

	if toolExecTimeout > natsauth.ResponsePermissionTTL-natsauth.ResponsePermissionMargin {
		return nil, fmt.Errorf("TOOL_EXEC_TIMEOUT must be at most %s to fit the NATS reply permission window", natsauth.ResponsePermissionTTL-natsauth.ResponsePermissionMargin)
	}

	healthCheckTimeout := 2 * time.Second
	if v := os.Getenv("HEALTH_CHECK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			healthCheckTimeout = d
		}
	}

	draftReplyModel := os.Getenv("DRAFT_REPLY_MODEL")
	if draftReplyModel == "" {
		draftReplyModel = model
	}

	conversationInputCap := defaultConversationInputCap
	if v := os.Getenv("LLM_CONVERSATION_INPUT_CAP"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			conversationInputCap = n
		}
	}
	conversationOutputCap := defaultConversationOutputCap
	if v := os.Getenv("LLM_CONVERSATION_OUTPUT_CAP"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			conversationOutputCap = n
		}
	}

	var freeTierDailySpendUSD float64
	if v := os.Getenv("LLM_FREE_TIER_DAILY_SPEND_USD"); v != "" {
		if f, perr := strconv.ParseFloat(v, 64); perr == nil {
			freeTierDailySpendUSD = f
		}
	}

	redisDownPolicy := redisDownPolicyBlock
	if v := os.Getenv("LLM_RATELIMIT_ON_REDIS_DOWN"); v != "" {
		switch v {
		case redisDownPolicyBlock, redisDownPolicyLocalFallback:
			redisDownPolicy = v
		default:
			return nil, fmt.Errorf("LLM_RATELIMIT_ON_REDIS_DOWN must be %q or %q, got %q",
				redisDownPolicyBlock, redisDownPolicyLocalFallback, v)
		}
	}

	localFallbackRequestsPerHour := defaultLocalFallbackRequestsPerHour
	if v := os.Getenv("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil {
			return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be a positive integer, got %q: %w", v, perr)
		}
		localFallbackRequestsPerHour = n
	}
	if redisDownPolicy == redisDownPolicyLocalFallback && localFallbackRequestsPerHour <= 0 {
		return nil, fmt.Errorf("LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR must be > 0 when LLM_RATELIMIT_ON_REDIS_DOWN=local_fallback")
	}

	localFallbackWindow := defaultLocalFallbackWindow
	if v := os.Getenv("LLM_LOCAL_FALLBACK_WINDOW"); v != "" {
		if d, perr := time.ParseDuration(v); perr == nil && d > 0 {
			localFallbackWindow = d
		}
	}

	allowTransborderLLM := false
	if v := os.Getenv("ALLOW_TRANSBORDER_LLM"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return nil, fmt.Errorf("ALLOW_TRANSBORDER_LLM must be a boolean, got %q: %w", v, perr)
		}
		allowTransborderLLM = b
	}

	enableGoogleBusiness := false
	if v := os.Getenv("ENABLE_GOOGLE_BUSINESS"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return nil, fmt.Errorf("ENABLE_GOOGLE_BUSINESS must be a boolean, got %q: %w", v, perr)
		}
		enableGoogleBusiness = b
	}

	imageGenEnabled := false
	if v := os.Getenv("IMAGE_GEN_ENABLED"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return nil, fmt.Errorf("IMAGE_GEN_ENABLED must be a boolean, got %q: %w", v, perr)
		}
		imageGenEnabled = b
	}

	imageGenMaxBytes := int64(defaultImageGenMaxBytes)
	if v := os.Getenv("IMAGE_GEN_MAX_BYTES"); v != "" {
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil || n <= 0 {
			return nil, fmt.Errorf("IMAGE_GEN_MAX_BYTES must be a positive integer, got %q", v)
		}
		imageGenMaxBytes = n
	}

	imageGenMaxPerTurn := defaultImageGenMaxPerTurn
	if v := os.Getenv("IMAGE_GEN_MAX_PER_TURN"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			return nil, fmt.Errorf("IMAGE_GEN_MAX_PER_TURN must be a non-negative integer (0 disables the cap), got %q", v)
		}
		imageGenMaxPerTurn = n
	}

	imageGenBucketTimeout := defaultImageGenBucketTimeout
	if v := os.Getenv("IMAGE_GEN_BUCKET_TIMEOUT"); v != "" {
		d, perr := time.ParseDuration(v)
		if perr != nil || d <= 0 {
			return nil, fmt.Errorf("IMAGE_GEN_BUCKET_TIMEOUT must be a positive duration (e.g. 10s), got %q", v)
		}
		imageGenBucketTimeout = d
	}

	s3UseSSL := false
	if v := os.Getenv("S3_USE_SSL"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return nil, fmt.Errorf("S3_USE_SSL must be a boolean, got %q: %w", v, perr)
		}
		s3UseSSL = b
	}

	return &Config{
		Port:                 getEnv("PORT", "8090"),
		LLMModel:             model,
		DraftReplyModel:      draftReplyModel,
		LLMTier:              getEnv("LLM_TIER", "free"),
		MaxIterations:        maxIter,
		MaxConcurrentStreams: maxStreams,
		NATSUrl:              getEnv("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout:      shutdownTimeout,
		ToolExecTimeout:      toolExecTimeout,
		HealthCheckTimeout:   healthCheckTimeout,

		MongoURI: getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:  getEnv("MONGO_DB", "onevoice"),

		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),

		APIInternalURL: getEnv("API_INTERNAL_URL", defaultAPIInternalURL),

		InternalSecret: os.Getenv("ORCHESTRATOR_INTERNAL_SECRET"),

		SelfHostedEndpoints: llm.ParseIndexedEndpoints(os.Getenv),

		RedisURL: getEnv("REDIS_URL", ""),

		FreeTierDailySpendUSD:        freeTierDailySpendUSD,
		ConversationInputCap:         conversationInputCap,
		ConversationOutputCap:        conversationOutputCap,
		RedisDownPolicy:              redisDownPolicy,
		LocalFallbackRequestsPerHour: localFallbackRequestsPerHour,
		LocalFallbackWindow:          localFallbackWindow,
		AllowTransborderLLM:          allowTransborderLLM,
		EnableGoogleBusiness:         enableGoogleBusiness,

		ImageGenEnabled:       imageGenEnabled,
		ImageGenProvider:      getEnv("IMAGE_GEN_PROVIDER", "openai"),
		ImageGenModel:         getEnv("IMAGE_GEN_MODEL", "dall-e-3"),
		ImageGenSize:          getEnv("IMAGE_GEN_SIZE", "1024x1024"),
		ImageGenMaxBytes:      imageGenMaxBytes,
		ImageGenMaxPerTurn:    imageGenMaxPerTurn,
		ImageGenBucketTimeout: imageGenBucketTimeout,

		YandexArtAPIKey:   os.Getenv("YANDEX_ART_API_KEY"),
		YandexArtFolderID: os.Getenv("YANDEX_ART_FOLDER_ID"),

		PublicURL:   os.Getenv("PUBLIC_URL"),
		S3Endpoint:  os.Getenv("S3_ENDPOINT"),
		S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey: os.Getenv("S3_SECRET_KEY"),
		S3Bucket:    getEnv("S3_BUCKET", "onevoice"),
		S3UseSSL:    s3UseSSL,
	}, nil
}

// RateLimiterDecision is the resolved outcome of the boot-time cost-guard
// gate. Enabled is true when REDIS_URL is present and the limiter will be
// wired; when false the orchestrator runs without a daily-spend / per-business
// cap (Degraded), which is only permitted outside production or with an
// explicit dev escape hatch.
type RateLimiterDecision struct {
	// Enabled is true when REDIS_URL is set and the rate limiter is wired.
	Enabled bool
	// Degraded is true when the limiter is intentionally off (no REDIS_URL).
	Degraded bool
}

// RateLimiterGate decides whether the orchestrator may boot with the LLM
// cost-guard disabled. Without Redis there is no daily-spend cap or per-
// business limit, so unbounded third-party LLM spend is possible.
//
// Rules:
//   - REDIS_URL set                       → Enabled (limiter wired).
//   - REDIS_URL unset, production          → boot error UNLESS
//     ALLOW_NO_RATE_LIMIT=true, in which case it boots Degraded.
//   - REDIS_URL unset, non-production      → boots Degraded (caller warns).
//
// Production is detected with the same APP_ENV=production idiom used across the
// codebase (services/api, agent-yandex-business).
func (c *Config) RateLimiterGate() (RateLimiterDecision, error) {
	if c.RedisURL != "" {
		return RateLimiterDecision{Enabled: true}, nil
	}
	isProd := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	escapeHatch := strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_NO_RATE_LIMIT")), "true")
	if isProd && !escapeHatch {
		return RateLimiterDecision{}, fmt.Errorf(
			"REDIS_URL is required in production: the LLM cost guard (daily-spend / per-business cap) cannot run without Redis; set REDIS_URL or ALLOW_NO_RATE_LIMIT=true to override (unbounded LLM spend)")
	}
	return RateLimiterDecision{Degraded: true}, nil
}

// IsProduction reports whether the orchestrator runs with APP_ENV=production —
// the same idiom RateLimiterGate and RequireInternalSecret use for their
// fail-closed branches, exposed so wiring code can gate on it too.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
}

// RequireInternalSecret decides whether the orchestrator may boot without the
// shared internal-inbound secret. The chat / resume / tool-registry /
// draft-reply routes trust attacker-controllable request bodies (UserID,
// BusinessID, Tier), so in production they must be reachable only by the api
// over an authenticated channel.
//
// Rules (mirroring RateLimiterGate's APP_ENV=production idiom):
//   - ORCHESTRATOR_INTERNAL_SECRET set, long enough → OK.
//   - set but shorter than InternalSecretMinLen        → boot error.
//   - unset, production                                → boot error.
//   - unset, non-production                            → OK (guard disabled).
func (c *Config) RequireInternalSecret() error {
	if c.InternalSecret != "" {
		if len(c.InternalSecret) < orchestratorclient.InternalSecretMinLen {
			return fmt.Errorf("ORCHESTRATOR_INTERNAL_SECRET must be at least %d characters (generate with: openssl rand -base64 32)", orchestratorclient.InternalSecretMinLen)
		}
		return nil
	}
	isProd := strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production")
	if isProd {
		return fmt.Errorf("ORCHESTRATOR_INTERNAL_SECRET is required in production: the orchestrator's internal inbound (chat/resume/draft-reply) trusts request-body identity and tier, so it must be reachable only over an authenticated channel; set ORCHESTRATOR_INTERNAL_SECRET (generate with: openssl rand -base64 32)")
	}
	return nil
}

// RedactMongoURI returns the Mongo URI with embedded user:password stripped, safe for logs.
// Returns "<mongo-uri-redacted>" on parse failure rather than leaking the raw value.
func (c *Config) RedactMongoURI() string {
	uri := c.MongoURI
	if uri == "" {
		return ""
	}
	schemeEnd := -1
	for i := 0; i+2 < len(uri); i++ {
		if uri[i] == ':' && uri[i+1] == '/' && uri[i+2] == '/' {
			schemeEnd = i + 3
			break
		}
	}
	if schemeEnd < 0 {
		return "<mongo-uri-redacted>"
	}
	atIdx := -1
	for i := schemeEnd; i < len(uri); i++ {
		switch uri[i] {
		case '@':
			atIdx = i
		case '/':
			if atIdx < 0 {
				return uri
			}
		}
	}
	if atIdx < 0 {
		return uri
	}
	return uri[:schemeEnd] + "***:***@" + uri[atIdx+1:]
}

// getEnv returns defaultValue when the env var is absent or empty.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
