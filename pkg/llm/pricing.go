package llm

import "strings"

// ModelRate is the per-1M-token USD list price for a single model.
type ModelRate struct {
	InputUSDPer1MTok  float64
	OutputUSDPer1MTok float64
}

// modelPricing is the single shared rate card consulted at registry-construction
// time (by both the api titler/draft-reply Router and the orchestrator chat
// Router) to stamp every ModelProviderEntry with real per-1M-token costs. It is
// the one source of truth in code — the two services used to keep divergent
// copies in their wire packages, which silently drifted and billed $0.
//
// Source of truth for the NUMBERS lives in docs/llm-pricing.md alongside the
// operator runbook. Adding a model means: (1) edit this map, (2) edit
// docs/llm-pricing.md, (3) extend TestPriceFor_KnownModel in pricing_test.go.
// Forgetting any one drops billing rows for that model to $0, which silently
// breaks the daily-spend rate limiter.
//
// As-of: DeepSeek row 2026-08-21 (see docs/llm-pricing.md "Last verified").
var modelPricing = map[string]ModelRate{
	"anthropic/claude-sonnet-4-6": {3.00, 15.00},
	"anthropic/claude-haiku-4-5":  {1.00, 5.00},
	"anthropic/claude-opus-4-7":   {5.00, 25.00},
	"openai/gpt-4o-mini":          {0.15, 0.60},
	// dall-e-3 is billed PER IMAGE, not per token — the authoritative flat list
	// price ($0.04 for a 1024×1024) lives in pkg/imagegen and is stamped
	// directly onto the generate_image usage row. This entry only keeps the
	// model id present in the rate card so it is never treated as an unknown
	// ($0-drift) model; the per-1M-token fields are not consulted for images.
	"dall-e-3": {0.04, 0.04},
	// DeepSeek V4 Flash via Yandex AI Studio (RU prod primary). Source: Yandex
	// AI Studio pricing, sync mode, verified 2026-08-21 — input 300 ₽/1M,
	// output 500 ₽/1M incl. VAT; converted at CBR 83.36 ₽/$ (2026-08-21). Model
	// IDs arrive folder-qualified (gpt://<folder>/deepseek-v4-flash/latest);
	// PriceFor normalizes them to this bare slug before lookup.
	"deepseek-v4-flash": {3.60, 6.00},
}

// PriceFor returns the (input, output) USD-per-1M-token list price for the given
// model ID. Unknown models return (0, 0) so the router still constructs without
// error but billing rows surface cost=0 — visible in usage_logs as the
// operator's drift signal that the rate card needs an update.
func PriceFor(modelID string) (inputUSDPer1MTok, outputUSDPer1MTok float64) {
	entry, ok := modelPricing[NormalizeModelID(modelID)]
	if !ok {
		return 0, 0
	}
	return entry.InputUSDPer1MTok, entry.OutputUSDPer1MTok
}

// NormalizeModelID reduces a provider-qualified model identifier to the bare
// slug used as a modelPricing key. Yandex AI Studio addresses models as
// gpt://<folder>/<model>[/<version>]; the folder segment is deployment-specific,
// so the rate card keys on <model> alone. Non-URI IDs pass through unchanged.
func NormalizeModelID(modelID string) string {
	rest, ok := strings.CutPrefix(modelID, "gpt://")
	if !ok {
		return modelID
	}
	if segs := strings.Split(rest, "/"); len(segs) >= 2 {
		return segs[1]
	}
	return modelID
}
