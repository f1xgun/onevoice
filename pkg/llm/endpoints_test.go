package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIndexedEndpoints(t *testing.T) {
	env := map[string]string{
		"SELF_HOSTED_0_URL":     "https://vm0/v1",
		"SELF_HOSTED_0_MODEL":   "llama3.1",
		"SELF_HOSTED_0_API_KEY": "sk-0",
		"SELF_HOSTED_1_URL":     "https://vm1/v1",
		"SELF_HOSTED_1_MODEL":   "mistral",
		// index 2 has URL but no MODEL → skipped, but scanning continues
		"SELF_HOSTED_2_URL": "https://vm2/v1",
		// index 3 present after a skipped 2 — proves we do not stop at a skip
		"SELF_HOSTED_3_URL":   "https://vm3/v1",
		"SELF_HOSTED_3_MODEL": "gemma",
	}
	lookup := func(k string) string { return env[k] }

	got := ParseIndexedEndpoints(lookup)

	require.Len(t, got, 3)
	assert.Equal(t, SelfHostedEndpoint{URL: "https://vm0/v1", Model: "llama3.1", APIKey: "sk-0"}, got[0])
	assert.Equal(t, SelfHostedEndpoint{URL: "https://vm1/v1", Model: "mistral", APIKey: ""}, got[1])
	assert.Equal(t, SelfHostedEndpoint{URL: "https://vm3/v1", Model: "gemma", APIKey: ""}, got[2])
}

func TestParseIndexedEndpoints_StopsAtFirstMissingURL(t *testing.T) {
	// A gap in URL (index 0 missing) stops the scan immediately → empty.
	got := ParseIndexedEndpoints(func(string) string { return "" })
	assert.Empty(t, got)
}
