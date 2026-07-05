package imagegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateParams_Valid(t *testing.T) {
	cases := []struct {
		size, style string
	}{
		{"", ""},
		{"1024x1024", ""},
		{"1792x1024", "vivid"},
		{"1024x1792", "natural"},
		{"", "vivid"},
	}
	for _, tc := range cases {
		assert.NoError(t, ValidateParams(tc.size, tc.style),
			"size=%q style=%q must be accepted", tc.size, tc.style)
	}
}

// TestValidateParams_InvalidSize proves an off-enum size is rejected as
// ErrInvalidParam BEFORE any paid call, and the raw value never becomes a
// silent default.
func TestValidateParams_InvalidSize(t *testing.T) {
	err := ValidateParams("512x512", "vivid")
	assert.ErrorIs(t, err, ErrInvalidParam)
	assert.Contains(t, err.Error(), "size")
}

func TestValidateParams_InvalidStyle(t *testing.T) {
	err := ValidateParams("1024x1024", "photographic")
	assert.ErrorIs(t, err, ErrInvalidParam)
	assert.Contains(t, err.Error(), "style")
}

// TestValidateParams_SizeCheckedFirst documents ordering: an invalid size is
// reported even when the style is also invalid.
func TestValidateParams_SizeCheckedFirst(t *testing.T) {
	err := ValidateParams("999x999", "bogus")
	assert.ErrorIs(t, err, ErrInvalidParam)
	assert.Contains(t, err.Error(), "size")
}
