package wire

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// findToolSpec returns the ToolSpec whose function name matches `name`.
func findToolSpec(t *testing.T, specs []toolregistry.ToolSpec, name string) toolregistry.ToolSpec {
	t.Helper()
	for _, s := range specs {
		if s.Def.Function.Name == name {
			return s
		}
	}
	t.Fatalf("tool spec %q not found", name)
	return toolregistry.ToolSpec{}
}

// TestUpdateInfoToolDoesNotAdvertiseWebsite guards against re-advertising the
// `website` field on yandex_business__update_info. The RPA layer has no DOM
// selector for it, so advertising it caused a website-only update to report a
// false "updated" while writing nothing. The field must be absent from the
// schema properties and from every description copy until a verified selector
// exists.
func TestUpdateInfoToolDoesNotAdvertiseWebsite(t *testing.T) {
	spec := findToolSpec(t, yandexTools(), tools.YandexBusinessUpdateInfo)

	props, ok := spec.Def.Function.Parameters["properties"].(map[string]interface{})
	require.True(t, ok, "update_info parameters must expose a properties map")

	_, hasWebsite := props["website"]
	assert.False(t, hasWebsite, "update_info schema must NOT advertise the unimplemented 'website' field")

	_, hasWebsiteDesc := spec.ParameterDescriptionsEn["website"]
	assert.False(t, hasWebsiteDesc, "update_info English parameter descriptions must NOT mention 'website'")

	assert.NotContains(t, strings.ToLower(spec.DescriptionEn), "website",
		"English tool description must NOT mention website")
	assert.NotContains(t, strings.ToLower(spec.Def.Function.Description), "сайт",
		"Russian tool description must NOT mention сайт")
	assert.NotContains(t, strings.ToLower(spec.UserDescription), "сайт",
		"Russian user-facing description must NOT mention сайт")

	_, hasPhone := props["phone"]
	_, hasDescription := props["description"]
	assert.True(t, hasPhone, "update_info must still advertise 'phone'")
	assert.True(t, hasDescription, "update_info must still advertise 'description'")
}
