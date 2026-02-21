package a2a

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithBusinessID_RoundTrip(t *testing.T) {
	ctx := WithBusinessID(context.Background(), "biz-123")
	got := BusinessIDFromContext(ctx)
	assert.Equal(t, "biz-123", got)
}

func TestBusinessIDFromContext_Missing(t *testing.T) {
	got := BusinessIDFromContext(context.Background())
	assert.Equal(t, "", got)
}
