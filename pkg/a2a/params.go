package a2a

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GetIntParam reads a numeric tool parameter from a Request.Args map.
// JSON-decoded numbers arrive as float64; this helper accepts that and
// returns the int value, or the provided default when the key is absent.
// LLMs frequently emit numeric ids as JSON strings, so a string value is
// parsed too (returning the default plus the parse error on malformed input).
// Returns an error for non-numeric types so callers can decide whether to
// reject or fall back silently.
func GetIntParam(args map[string]any, key string, defaultValue int) (int, error) {
	v, ok := args[key]
	if !ok {
		return defaultValue, nil
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return defaultValue, err
		}
		return int(i), nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return defaultValue, fmt.Errorf("param %q: %w", key, err)
		}
		return i, nil
	default:
		return defaultValue, fmt.Errorf("param %q: expected number, got %T", key, v)
	}
}
