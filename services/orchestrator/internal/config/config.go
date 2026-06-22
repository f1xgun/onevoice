package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultShutdownTimeout is the fallback graceful-shutdown budget when SHUTDOWN_TIMEOUT is unset.
const defaultShutdownTimeout = 30 * time.Second

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
	NATSUrl         string
	ShutdownTimeout time.Duration
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

	SelfHostedEndpoints []SelfHostedEndpoint

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
}

// SelfHostedEndpoint holds one self-hosted LLM inference endpoint.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
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

	return &Config{
		Port:               getEnv("PORT", "8090"),
		LLMModel:           model,
		DraftReplyModel:    draftReplyModel,
		LLMTier:            getEnv("LLM_TIER", "free"),
		MaxIterations:      maxIter,
		NATSUrl:            getEnv("NATS_URL", "nats://localhost:4222"),
		ShutdownTimeout:    shutdownTimeout,
		ToolExecTimeout:    toolExecTimeout,
		HealthCheckTimeout: healthCheckTimeout,

		MongoURI: getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:  getEnv("MONGO_DB", "onevoice"),

		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),

		APIInternalURL: getEnv("API_INTERNAL_URL", defaultAPIInternalURL),

		SelfHostedEndpoints: parseIndexedEndpoints(),

		RedisURL: getEnv("REDIS_URL", ""),

		FreeTierDailySpendUSD:        freeTierDailySpendUSD,
		ConversationInputCap:         conversationInputCap,
		ConversationOutputCap:        conversationOutputCap,
		RedisDownPolicy:              redisDownPolicy,
		LocalFallbackRequestsPerHour: localFallbackRequestsPerHour,
		LocalFallbackWindow:          localFallbackWindow,
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

// parseIndexedEndpoints scans SELF_HOSTED_N_URL/_MODEL/_API_KEY for N=0,1,…
// stopping at the first missing _URL; entries without _MODEL are skipped.
func parseIndexedEndpoints() []SelfHostedEndpoint {
	var result []SelfHostedEndpoint
	for i := 0; ; i++ {
		prefix := fmt.Sprintf("SELF_HOSTED_%d_", i)
		url := os.Getenv(prefix + "URL")
		if url == "" {
			break
		}
		model := os.Getenv(prefix + "MODEL")
		if model == "" {
			continue
		}
		result = append(result, SelfHostedEndpoint{
			URL:    url,
			Model:  model,
			APIKey: os.Getenv(prefix + "API_KEY"),
		})
	}
	return result
}

// getEnv returns defaultValue when the env var is absent or empty.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
