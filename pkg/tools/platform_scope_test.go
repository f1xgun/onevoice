package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSelectedPlatforms(t *testing.T) {
	got, err := NormalizeSelectedPlatforms([]string{"vk", "telegram", "vk"})
	require.NoError(t, err)
	assert.Equal(t, []string{"vk", "telegram"}, got)

	for _, input := range [][]string{
		{},
		{"google_business"},
		{"telegram", "vk", "yandex_business", "telegram"},
	} {
		_, err := NormalizeSelectedPlatforms(input)
		require.Error(t, err)
	}
}

func TestIntersectSelectedPlatforms_EmptyRemainsClosed(t *testing.T) {
	got := IntersectSelectedPlatforms([]string{"vk"}, []string{"telegram"})
	require.NotNil(t, got)
	assert.Empty(t, got)
}
