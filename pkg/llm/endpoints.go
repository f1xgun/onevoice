package llm

import "fmt"

// SelfHostedEndpoint holds one OpenAI-compatible ("self-hosted") LLM inference
// endpoint parsed from the SELF_HOSTED_N_* env vars. Shared by services/api and
// services/orchestrator (both register these against the Router) so the two
// service configs cannot drift apart.
type SelfHostedEndpoint struct {
	URL    string
	Model  string
	APIKey string // optional
}

// ParseIndexedEndpoints scans SELF_HOSTED_N_URL / _MODEL / _API_KEY for
// N = 0, 1, 2, … using the supplied lookup (pass os.Getenv), stopping at the
// first missing _URL. Entries without _MODEL are skipped. The env lookup is
// injected so this stays a pure function and pkg/llm keeps no direct dependency
// on os — services call it as llm.ParseIndexedEndpoints(os.Getenv).
func ParseIndexedEndpoints(lookup func(string) string) []SelfHostedEndpoint {
	var result []SelfHostedEndpoint
	for i := 0; ; i++ {
		prefix := fmt.Sprintf("SELF_HOSTED_%d_", i)
		url := lookup(prefix + "URL")
		if url == "" {
			break
		}
		model := lookup(prefix + "MODEL")
		if model == "" {
			continue
		}
		result = append(result, SelfHostedEndpoint{
			URL:    url,
			Model:  model,
			APIKey: lookup(prefix + "API_KEY"),
		})
	}
	return result
}
