package llm

// Config represents llm.yaml configuration
type Config struct {
	Providers        map[string]ProviderConfig `yaml:"providers"`
	Commission       CommissionConfig          `yaml:"commission"`
	ModelFilter      ModelFilterConfig         `yaml:"model_filter"`
	PricingOverrides map[string]PricingInfo    `yaml:"pricing_overrides"`
	DefaultPricing   PricingInfo               `yaml:"default_pricing"`
}

// ProviderConfig configures a single provider
type ProviderConfig struct {
	Enabled   bool   `yaml:"enabled"`
	APIKeyEnv string `yaml:"api_key_env"`
	Priority  int    `yaml:"priority"`
}

// CommissionConfig defines platform markup strategy
type CommissionConfig struct {
	Mode       string             `yaml:"mode"` // "percentage", "flat", "tiered"
	Percentage float64            `yaml:"percentage"`
	FlatFeeUSD float64            `yaml:"flat_fee_usd"`
	Tiered     map[string]float64 `yaml:"tiered"` // tier -> percentage
}

// ModelFilterConfig controls which models to enable
type ModelFilterConfig struct {
	Mode      string   `yaml:"mode"` // "whitelist", "blacklist", "all"
	Whitelist []string `yaml:"whitelist"`
	Blacklist []string `yaml:"blacklist"`
}

// PricingInfo defines model pricing
type PricingInfo struct {
	InputPer1M  float64 `yaml:"input_per_1m"`
	OutputPer1M float64 `yaml:"output_per_1m"`
}
