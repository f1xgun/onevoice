package audit

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	id := uuid.New()
	s := EncodeCursor(now, id)
	require.NotEmpty(t, s)
	require.NotContains(t, s, "=")
	got, gotID, err := DecodeCursor(s)
	require.NoError(t, err)
	require.True(t, got.Equal(now), "time mismatch %v vs %v", got, now)
	require.Equal(t, id, gotID)
}

func TestCursorRoundTrip_PreservesNanoSecond(t *testing.T) {
	now := time.Date(2026, 5, 22, 10, 30, 45, 123456789, time.UTC)
	id := uuid.New()
	s := EncodeCursor(now, id)
	got, _, err := DecodeCursor(s)
	require.NoError(t, err)
	require.True(t, got.Equal(now))
	require.Equal(t, 123456789, got.Nanosecond())
}

func TestDecodeCursor_Malformed(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not_base64":   "%%%%%%",
		"not_json":     base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte("hello world")),
		"missing_t":    encB64(`{"id":"00000000-0000-0000-0000-000000000001"}`),
		"missing_id":   encB64(`{"t":"2026-01-01T00:00:00Z"}`),
		"bad_time":     encB64(`{"t":"not-a-time","id":"00000000-0000-0000-0000-000000000001"}`),
		"bad_uuid":     encB64(`{"t":"2026-01-01T00:00:00Z","id":"not-a-uuid"}`),
		"empty_fields": encB64(`{"t":"","id":""}`),
	}
	for name, s := range cases {
		_, _, err := DecodeCursor(s)
		require.Truef(t, errors.Is(err, ErrInvalidCursor),
			"%s: expected ErrInvalidCursor, got %v", name, err)
	}
}

// encB64 mirrors the encoder used by EncodeCursor (URL-safe, no padding).
// Kept tiny + inline so the tests don't depend on package internals.
func encB64(s string) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(s))
}
