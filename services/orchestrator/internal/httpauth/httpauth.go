// Package httpauth guards the orchestrator's cluster-internal inbound. The
// chat / resume / tool-registry / draft-reply routes trust attacker-controllable
// request bodies (UserID, BusinessID, Tier), so they must only be reachable by
// the api. This middleware authenticates the caller with a shared secret; the
// api stamps the same secret via pkg/orchestratorclient.WithInternalSecret.
package httpauth

import (
	"crypto/subtle"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// InternalSecret returns middleware that rejects any request whose
// InternalSecretHeader does not match secret, using a constant-time compare so
// a valid secret cannot be recovered by timing the response.
//
// An empty secret disables the guard and the middleware becomes a pass-through
// — this is the dev/test posture, gated fail-closed in production by the
// orchestrator's config (RequireInternalSecret). Health and metrics routes are
// wired outside this middleware so operator/liveness probes never carry the
// secret.
func InternalSecret(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if secret == "" {
			return next
		}
		want := []byte(secret)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(orchestratorclient.InternalSecretHeader))
			if subtle.ConstantTimeCompare(got, want) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
