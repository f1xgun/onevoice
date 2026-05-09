package vkapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveAPIVersion_Default(t *testing.T) {
	t.Setenv("VK_API_VERSION", "")
	assert.Equal(t, DefaultAPIVersion, resolveAPIVersion())
}

func TestResolveAPIVersion_Override(t *testing.T) {
	t.Setenv("VK_API_VERSION", "5.250")
	assert.Equal(t, "5.250", resolveAPIVersion())
}
