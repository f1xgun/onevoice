package audit

import "errors"

// ErrInvalidCursor is returned by DecodeCursor when the cursor string is
// empty, not base64, not JSON, or missing required fields. The REST handler
// maps this to HTTP 400 invalid_cursor (never 500).
var ErrInvalidCursor = errors.New("audit: invalid cursor")

// ErrInvalidCategory is returned when a filter category is not in the
// closed set {rbac, auth, integration, business, project}. The REST handler
// maps this to HTTP 400 invalid_category.
var ErrInvalidCategory = errors.New("audit: invalid category")

// ErrInvalidAction is returned when a filter action is not a known Action*
// constant. The REST handler maps this to HTTP 400 invalid_action.
var ErrInvalidAction = errors.New("audit: invalid action")
