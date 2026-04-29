// Package yandexcookies normalizes user-pasted Yandex session cookies
// into the canonical JSON-array shape consumed by the agent's Playwright
// pool (services/agent-yandex-business injectCookies).
//
// Three input formats are accepted:
//
//  1. JSON array (Cookie-Editor / EditThisCookie export):
//     [{"name":"Session_id","value":"3:..."},{"name":"sessionid2","value":"..."}]
//
//  2. Raw "Cookie:" header (Chrome DevTools "Copy as cURL"):
//     Session_id=3:...; sessionid2=...; yandex_login=...
//
//  3. Single Session_id pair or bare value:
//     Session_id=3:...
//     3:1234567890.5.0.1234567890123:abcXYZ:1.1|...
//
// All forms are normalized to the JSON-array shape with domain=".yandex.ru"
// and path="/" defaults. Session_id is required; everything else is optional
// but preserved when present.
package yandexcookies

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Cookie is the canonical shape passed to Playwright AddCookies.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// Parsed is the result of a successful parse.
type Parsed struct {
	Cookies []Cookie
	Format  string // "json", "cookie_header", "session_id_value"
}

// JSON returns the canonical JSON-array representation that the agent's
// injectCookies expects.
func (p Parsed) JSON() string {
	b, _ := json.Marshal(p.Cookies)
	return string(b)
}

// Errors surfaced to the user verbatim.
var (
	ErrEmpty            = errors.New("вставьте cookies из браузера")
	ErrNoSessionID      = errors.New("не найдено значение Session_id — это главный cookie для входа в Яндекс")
	ErrInvalidJSON      = errors.New("не удалось распознать формат: это не JSON, не Cookie-заголовок и не значение Session_id")
	ErrSessionIDInvalid = errors.New("значение Session_id выглядит некорректно — проверьте, что скопировали целиком")
)

// Parse normalizes a user-supplied cookies string. It tries the three
// supported formats in order and returns the first that produces a usable
// Session_id. Whitespace at the boundaries is trimmed; the input is
// otherwise treated literally.
func Parse(input string) (Parsed, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Parsed{}, ErrEmpty
	}

	// Format 1: JSON array
	if strings.HasPrefix(trimmed, "[") {
		cookies, err := parseJSONArray(trimmed)
		if err != nil {
			return Parsed{}, err
		}
		if err := requireSessionID(cookies); err != nil {
			return Parsed{}, err
		}
		return Parsed{Cookies: cookies, Format: "json"}, nil
	}

	// Format 2: bare Session_id value (no "=" sign at all)
	if !strings.Contains(trimmed, "=") {
		if looksLikeSessionIDValue(trimmed) {
			return Parsed{
				Cookies: []Cookie{{Name: "Session_id", Value: trimmed, Domain: ".yandex.ru", Path: "/"}},
				Format:  "session_id_value",
			}, nil
		}
		return Parsed{}, ErrInvalidJSON
	}

	// Format 3: cookie header. Covers both the single "Session_id=..." pair
	// and the multi-cookie "k=v; k=v; ..." string.
	cookies, ok := parseCookieHeader(trimmed)
	if !ok {
		return Parsed{}, ErrInvalidJSON
	}
	if err := requireSessionID(cookies); err != nil {
		return Parsed{}, err
	}
	return Parsed{Cookies: cookies, Format: "cookie_header"}, nil
}

// parseJSONArray accepts the Cookie-Editor / EditThisCookie export shape:
// an array of objects with at least name+value. Domain and path are
// optional and default to .yandex.ru / "/".
func parseJSONArray(input string) ([]Cookie, error) {
	var raw []map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return nil, fmt.Errorf("ошибка JSON: %w", err)
	}
	cookies := make([]Cookie, 0, len(raw))
	for i, c := range raw {
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		if name == "" || value == "" {
			continue
		}
		domain, _ := c["domain"].(string)
		if domain == "" {
			domain = ".yandex.ru"
		}
		path, _ := c["path"].(string)
		if path == "" {
			path = "/"
		}
		cookies = append(cookies, Cookie{Name: name, Value: value, Domain: domain, Path: path})
		_ = i
	}
	return cookies, nil
}

// parseCookieHeader splits a "k=v; k=v" string. It tolerates a leading
// "Cookie:" prefix that users may copy along with the header.
func parseCookieHeader(input string) ([]Cookie, bool) {
	s := input
	if i := strings.Index(strings.ToLower(s), "cookie:"); i == 0 {
		s = strings.TrimSpace(s[len("cookie:"):])
	}
	parts := strings.Split(s, ";")
	cookies := make([]Cookie, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(p[:eq])
		value := strings.TrimSpace(p[eq+1:])
		if name == "" || value == "" {
			continue
		}
		cookies = append(cookies, Cookie{
			Name:   name,
			Value:  value,
			Domain: ".yandex.ru",
			Path:   "/",
		})
	}
	if len(cookies) == 0 {
		return nil, false
	}
	return cookies, true
}

// requireSessionID ensures the parsed list carries a non-empty Session_id.
// Without it the cookies cannot authenticate against Yandex Passport.
func requireSessionID(cookies []Cookie) error {
	for _, c := range cookies {
		if strings.EqualFold(c.Name, "Session_id") {
			if !looksLikeSessionIDValue(c.Value) {
				return ErrSessionIDInvalid
			}
			return nil
		}
	}
	return ErrNoSessionID
}

// looksLikeSessionIDValue applies a coarse shape check on a candidate
// Session_id. Yandex's Session_id has the form "3:<unix_ts>.<n>.<n>.<n>:<base64ish>:<n>.<n>|<base64ish>",
// always starts with "3:", and is at least 50 chars long. We don't validate
// the inner structure precisely — Yandex changes it — we just guard against
// obviously wrong pastes (e.g. "ABC123", "true", "Session_id").
func looksLikeSessionIDValue(s string) bool {
	if len(s) < 50 {
		return false
	}
	return strings.HasPrefix(s, "3:")
}
