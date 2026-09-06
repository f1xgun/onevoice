package llm

import (
	"fmt"
	"strings"
)

// EnforceResidency is the production data-residency gate for LLM routing.
//
// The operator's legal position (152-ФЗ) is that prompt contents — which carry
// business and customer personal data — stay inside the Russian perimeter
// unless a cross-border transfer has been filed and consented. Hosted
// providers (OpenRouter, OpenAI, Anthropic) run outside that perimeter, so in
// production they are refused unless allowTransborder is explicitly set, and
// every configured model must be served by one of the SELF_HOSTED_N_*
// endpoints (which the deploy checklist points at RF-hosted inference).
//
// hostedKeysSet lists the env-var names of hosted-provider keys that are
// non-empty; it is only used for the error message. Outside production, or
// when allowTransborder is true, the gate is a no-op so local development and
// a deliberately filed cross-border setup keep working.
func EnforceResidency(production, allowTransborder bool, hostedKeysSet, models []string, endpoints []SelfHostedEndpoint) error {
	if !production || allowTransborder {
		return nil
	}
	if len(hostedKeysSet) > 0 {
		return fmt.Errorf("LLM residency: hosted provider keys set in production (%s) would route prompts outside the RF perimeter; unset them and serve every model from SELF_HOSTED_N_* endpoints, or set ALLOW_TRANSBORDER_LLM=true only with a filed cross-border legal basis",
			strings.Join(hostedKeysSet, ", "))
	}
	served := make(map[string]struct{}, len(endpoints))
	for _, ep := range endpoints {
		served[ep.Model] = struct{}{}
	}
	for _, m := range models {
		if m == "" {
			continue
		}
		if _, ok := served[m]; !ok {
			return fmt.Errorf("LLM residency: model %q is not served by any SELF_HOSTED_N_* endpoint; in production every configured model must resolve to an RF-hosted endpoint (or set ALLOW_TRANSBORDER_LLM=true with a filed legal basis)", m)
		}
	}
	return nil
}

// HostedKeysSet returns the env-var names of the hosted-provider keys that are
// configured, in a stable order, for EnforceResidency's error message.
func HostedKeysSet(openRouter, openAI, anthropic string) []string {
	var set []string
	if openRouter != "" {
		set = append(set, "OPENROUTER_API_KEY")
	}
	if openAI != "" {
		set = append(set, "OPENAI_API_KEY")
	}
	if anthropic != "" {
		set = append(set, "ANTHROPIC_API_KEY")
	}
	return set
}
