package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultMaxTokensFor_KnownAndUnknownModels(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-sonnet-4-6", 8192},
		{"claude-haiku-4-5", 4096},
		{"claude-haiku-4-5-20251001", 4096},
		{"claude-opus-4-7", 8192},
		{"claude-opus-4-6", 8192},
		{"claude-sonnet-4-5", 8192},
		{"claude-sonnet-4-5-20250929", 8192},
		{"some-future-model", 4096},
		{"", 4096},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, defaultMaxTokensFor(tc.model))
		})
	}
}
