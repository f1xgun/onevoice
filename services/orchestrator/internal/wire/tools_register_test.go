package wire

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

func registeredToolNames(t *testing.T, enableGoogleBusiness bool) []string {
	t.Helper()
	reg := toolregistry.NewRegistry()
	RegisterPlatformTools(reg, nil, enableGoogleBusiness)
	defs := reg.Available([]string{"telegram", "vk", "yandex_business", "google_business"})
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	return names
}

func hasPlatformTool(names []string, platformPrefix string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, platformPrefix) {
			return true
		}
	}
	return false
}

func TestRegisterPlatformTools_GoogleDisabledByDefault(t *testing.T) {
	names := registeredToolNames(t, false)

	assert.True(t, hasPlatformTool(names, "telegram__"), "telegram tools must register")
	assert.True(t, hasPlatformTool(names, "vk__"), "vk tools must register")
	assert.True(t, hasPlatformTool(names, "yandex_business__"), "yandex tools must register")
	assert.False(t, hasPlatformTool(names, "google_business__"),
		"google tools must NOT register when disabled")
}

func TestRegisterPlatformTools_GoogleEnabled(t *testing.T) {
	names := registeredToolNames(t, true)

	assert.True(t, hasPlatformTool(names, "google_business__"),
		"google tools must register when enabled")
}
