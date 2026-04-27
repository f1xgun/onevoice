package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPhase19SearchErrors — Plan 19-03 / Wave 0. Asserts the new search
// sentinels exist and behave as identity-comparable error values (the
// canonical way every callsite distinguishes them via errors.Is).
func TestPhase19SearchErrors(t *testing.T) {
	t.Run("ErrInvalidScope is a non-nil error value", func(t *testing.T) {
		require.NotNil(t, ErrInvalidScope)
		assert.True(t, errors.Is(ErrInvalidScope, ErrInvalidScope))
	})
	t.Run("ErrSearchIndexNotReady is a non-nil error value", func(t *testing.T) {
		require.NotNil(t, ErrSearchIndexNotReady)
		assert.True(t, errors.Is(ErrSearchIndexNotReady, ErrSearchIndexNotReady))
	})
	t.Run("the two sentinels are distinct", func(t *testing.T) {
		assert.False(t, errors.Is(ErrInvalidScope, ErrSearchIndexNotReady))
		assert.False(t, errors.Is(ErrSearchIndexNotReady, ErrInvalidScope))
	})
	t.Run("error messages are stable for log audits", func(t *testing.T) {
		assert.Contains(t, ErrInvalidScope.Error(), "invalid scope")
		assert.Contains(t, ErrSearchIndexNotReady.Error(), "index not ready")
	})
}
