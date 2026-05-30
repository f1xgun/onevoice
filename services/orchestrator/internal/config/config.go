package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// defaultShutdownTimeout is the fallback graceful-shutdown budget when no
// SHUTDOWN_TIMEOUT env override is provided. 30s gives in-flight LLM and
// tool-dispatch requests time to drain before SIGKILL.
const defaultShutdownTimeout = 30 * time.Second

// defaultAPIInternalURL is the in-cluster mTLS endpoint the orchestrator dials
// for internal API calls (billing usage_logs, future internal endpoints) when
// API_INTERNAL_URL env is unset. Matches docker-compose service DNS + the API's
// internal :8443 listener.
const defaultAPIInternalURL = "https://api:8443"

// Cost-guard defaults. These are documented in .env.example and
// docs/llm-cost-guards.md; operators tune via env.
const (
	// defaultConversationInputCap stops the agent loop once the running input
	// budget for a single conversation hits this many tokens.
	defaultConversationInputCap = 50000

	// defaultConversationOutputCap is the parallel limit on output tokens.
	defaultConversationOutputCap = 10000

	// defaultLocalFallbackRequestsPerHour roughly matches the "$10/h" ceiling
	// at the v1.4 average per-request cost (~$0.005). Operators tune in
	// docs/llm-cost-guards.md.
	defaultLocalFallbackRequestsPerHour = 2000

	// defaultLocalFallbackWindow is how long the limiter consults the in-
	// process bucket before re-probing Redis.
	defaultLocalFallbackWindow = 30 * time.Second

	// redisDownPolicyBlock / redisDownPolicyLocalFallback are the literal
	// values accepted by LLM_RATELIMIT_ON_REDIS_DOWN.
	redisDownPolicyBlock         = "block"
	redisDownPolicyLocalFallback = "local_fallback"
)

// Config holds orchestrator configuration loaded from environment.
type Config struct {
	Port     string
	LLMModel string
	// DraftReplyModel is a cheap-tier model used to draft AI-suggested replies
	// in the review_sync pipeline (services/orchestrator/internal/handler/
	// draft_reply.go). Falls back to LLMModel when DRAFT_REPLY_MODEL env is
	// unset, which mirrors the TitlerModel fallback in
	// services/api/internal/config/config.go::Load. The two cheap-tier
	// callsites are kept separate (rather than a unified CHEAP_MODEL var) so a
	// future tuning round can route titler at one model and draft_reply at
	// another without API surface churn. DraftReply does not use tools —
	// routing it at any chat-completion model is safe.
	DraftReplyModel string
	LLMTier         string
	MaxIterations   int
	NATSUrl         string
	ShutdownTimeout time.Duration
	// ToolExecTimeout bounds a single tool call. Zero disables the per-tool
	// deadline — the request context still governs overall cancellation.
	ToolExecTimeout time.Duration

	// HealthCheckTimeout caps any single dep ping inside /health/ready.
	// Checks run concurrently (pkg/health.ReadyHandler), so total wall-clock
	// budget = HealthCheckTimeout (max, not sum). Default 2s.
	HealthCheckTimeout time.Duration

	// MongoDB connection. The
	// orchestrator writes pending_tool_calls batches at pause time, so it
	// needs its own Mongo connection (avoids a circular dependency where
	// orchestrator → API → orchestrator). Defaults match the API service's
	// docker-compose values so dev setups that only set one MONGO_URI
	// continue to work.
	MongoURI string
	MongoDB  string

	// LLM provider API keys (at least one must be set)
	OpenRouterAPIKey string
	OpenAIAPIKey     string
	AnthropicAPIKey  string

	// APIInternalURL is the base URL of the API service's mTLS-protected
	// internal :8443 listener. pkg/billingclient is wired against it for the
	// orchestrator → api billing POST hop. Defaults to "https://api:8443"
	// which matches the docker-compose env contract. Must be HTTPS — the
	// mTLS substrate requires it on this endpoint.
	APIInternalURL string

	SelfHostedEndpoints []SelfHostedEndpoint

	// RedisURL is the connection string the orchestrator dials for the
	// rate-limiter's per-minute / per-month counters. Empty disables the
	// rate-limiter wiring at boot.
	RedisURL string

	// Cost-guard knobs.
	//
	// FreeTierDailySpendUSD overrides DefaultTierLimits["free"].DailySpendUSD
	// at wire time. 0 keeps the compiled default; -1 disables the gate
	// (unlimited); positive sets the dollar cap.
	FreeTierDailySpendUSD float64

	// ConversationInputCap / ConversationOutputCap configure the per-
	// conversation agent-loop token budget. 0 disables that axis.
	ConversationInputCap  int
	ConversationOutputCap int

	// RedisDownPolicy selects the behavior when Redis fails: "block"
	// (default, fail-closed) or "local_fallback" (in-process bucket).
	RedisDownPolicy string

	// LocalFallbackRequestsPerHour is the bucket rate when policy is
	// local_fallback. Required (>0) when policy is local_fallback.
	LocalFallbackRequestsPerHour int

	// LocalFallbackWindow is how long after a Redis failure the limiter
	// consults the in-process bucket before retrying Redis. Default 30s.
	LocalFallbackWindow time.Duration
}

// SelfHostedEndpoint holds configuration for one self-hosted LLM inference endpoint.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}

// Load reads config from environment variables.
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

	var toolExecTimeout time.Duration
	if v := os.Getenv("TOOL_EXEC_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			toolExecTimeout = d
		}
	}

	// Per-dep readiness check timeout. Defensive default + clamp so an
	// operator typo (or zero/negative explicit value) can't disable the
	// safety net. Matches the API service's HealthCheckTimeout semantics.
	healthCheckTimeout := 2 * time.Second
	if v := os.Getenv("HEALTH_CHECK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			healthCheckTimeout = d
		}
	}

	// DraftReplyModel mirrors the TitlerModel fallback pattern in
	// services/api/internal/config/config.go: explicit DRAFT_REPLY_MODEL wins;
	// otherwise reuse LLMModel so the review_sync drafter still has a model
	// to call against. We resolve the fallback at Load time (not inside the
	// handler) so the redacted startup log records the effective model.
	draftReplyModel := os.Getenv("DRAFT_REPLY_MODEL")
	if draftReplyModel == "" {
		draftReplyModel = model
	}

	// Cost-guard knobs. Parse defensively: parse errors keep the default and
	// log a warning; semantically invalid combinations (unknown policy, or
	// local_fallback with zero rate) are hard boot errors so a misconfigured
	// deploy refuses to start instead of silently disabling the limiter.
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
		if n, perr := strconv.Atoi(v); perr == nil {
			localFallbackRequestsPerHour = n
		}
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

// RedactMongoURI returns the Mongo connection URI with any embedded user:
// password stripped, suitable for logging on startup. Implementation is
// intentionally conservative: if the URI fails to parse it returns the
// string `<mongo-uri-redacted>` rather than leaking the raw value.
func (c *Config) RedactMongoURI() string {
	// Supported forms: mongodb://user:pass@host[:port]/db and
	// mongodb+srv://user:pass@host/db. We only redact the user-info segment
	// between "//" and "@"; everything after "@" is host/path which is
	// non-sensitive.
	uri := c.MongoURI
	if uri == "" {
		return ""
	}
	// Find the scheme separator.
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
				// Path started before any '@' — no user-info segment. Safe as-is.
				return uri
			}
		}
	}
	if atIdx < 0 {
		// No user-info segment. Safe as-is.
		return uri
	}
	return uri[:schemeEnd] + "***:***@" + uri[atIdx+1:]
}

// parseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY env vars
// for N = 0, 1, 2, … stopping when SELF_HOSTED_N_URL is missing.
// Entries without MODEL are skipped.
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

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
