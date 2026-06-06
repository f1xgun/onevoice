package audit

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// cursorPayload is the JSON shape inside the base64 wrapper. Field names
// are short to keep the cursor opaque-but-tiny.
type cursorPayload struct {
	T  string `json:"t"`  // RFC3339Nano-formatted created_at
	ID string `json:"id"` // uuid.String()
}

// cursorEncoding is the single source of truth for the cursor's base64
// flavor. URL-safe + no padding so the token is safe in URL query strings
var cursorEncoding = base64.URLEncoding.WithPadding(base64.NoPadding)

// EncodeCursor builds the opaque pagination token from the last row's
// (created_at, id). Returned token is URL-safe with no padding.
func EncodeCursor(t time.Time, id uuid.UUID) string {
	p := cursorPayload{T: t.UTC().Format(time.RFC3339Nano), ID: id.String()}
	b, _ := json.Marshal(p)
	return cursorEncoding.EncodeToString(b)
}

// DecodeCursor parses an EncodeCursor output. Returns ErrInvalidCursor on
// any malformed input — caller MUST map to HTTP 400 (never 500).
func DecodeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	b, err := cursorEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	var p cursorPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	if p.T == "" || p.ID == "" {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, p.T)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return t, id, nil
}
