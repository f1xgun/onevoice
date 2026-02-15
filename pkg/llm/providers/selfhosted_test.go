package providers_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/llm/providers"
	"github.com/stretchr/testify/assert"
)

func TestSelfHostedProvider_Name(t *testing.T) {
	p := providers.NewSelfHosted("selfhosted-0", "http://localhost:11434/v1", "")
	assert.Equal(t, "selfhosted-0", p.Name())
}
