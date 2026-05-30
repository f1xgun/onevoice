package llm

import "testing"

func TestDefaultMaxTokensFor(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-6", maxTokensSonnetDefault},
		{"anthropic/claude-sonnet-4-6", maxTokensSonnetDefault},
		{"claude-haiku-4-5", maxTokensHaikuDefault},
		{"anthropic/claude-haiku-4-5", maxTokensHaikuDefault},
		{"claude-opus-4-7", maxTokensOpusDefault},
		{"anthropic/claude-opus-4-7", maxTokensOpusDefault},
		{"openai/gpt-4o-mini", maxTokensUnknownDefault},
		{"", maxTokensUnknownDefault},
		{"some-future-model", maxTokensUnknownDefault},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := DefaultMaxTokensFor(tc.model); got != tc.want {
				t.Fatalf("DefaultMaxTokensFor(%q) = %d, want %d", tc.model, got, tc.want)
			}
		})
	}
}
