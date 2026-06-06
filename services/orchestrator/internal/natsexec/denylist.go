package natsexec

import "strings"

var deniedKeys = map[string]struct{}{
	"cookies":       {},
	"session_id":    {},
	"token":         {},
	"auth":          {},
	"authorization": {},
	"password":      {},
	"api_key":       {},
}

// violatesDenyList reports whether the tool args contain a deny-listed key.
// Exact (case-insensitive) match on map keys. Substring/prefix matches do not
// trigger. Nested map[string]interface{} is walked recursively; arrays are not
// (deferred). On a hit it returns the original (un-lowercased) key.
func violatesDenyList(args map[string]interface{}) (bool, string) {
	return walkDeny(args)
}

func walkDeny(m map[string]interface{}) (bool, string) {
	for k, v := range m {
		if _, denied := deniedKeys[strings.ToLower(k)]; denied {
			return true, k
		}
		if nested, ok := v.(map[string]interface{}); ok {
			if hit, key := walkDeny(nested); hit {
				return true, key
			}
		}
	}
	return false, ""
}
