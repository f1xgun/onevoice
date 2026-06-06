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
// trigger. Nested map[string]interface{} and []interface{} are walked
// recursively so a deny-listed key buried in an array element is still caught.
// On a hit it returns the original (un-lowercased) key.
func violatesDenyList(args map[string]interface{}) (bool, string) {
	return walkDeny(args)
}

func walkDeny(m map[string]interface{}) (bool, string) {
	for k, v := range m {
		if _, denied := deniedKeys[strings.ToLower(k)]; denied {
			return true, k
		}
		if hit, key := walkValue(v); hit {
			return true, key
		}
	}
	return false, ""
}

func walkValue(v interface{}) (bool, string) {
	switch t := v.(type) {
	case map[string]interface{}:
		return walkDeny(t)
	case []interface{}:
		for _, e := range t {
			if hit, key := walkValue(e); hit {
				return true, key
			}
		}
	}
	return false, ""
}
