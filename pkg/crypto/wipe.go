package crypto

import "runtime"

// Wipe zero-fills b in place. Callers should defer Wipe immediately after a
// sensitive byte slice is materialized to bound the in-memory exposure window.
// Strings cannot be wiped; sensitive material must stay as []byte until Wipe.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
	runtime.KeepAlive(b)
}
