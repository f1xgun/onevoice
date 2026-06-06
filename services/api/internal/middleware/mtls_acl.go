package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

// RequirePlatformACL gates an internal route by the client cert CommonName AND
// the requested platform query parameter. It is the second factor on top of the
// mTLS handshake: the handshake establishes WHO the caller is (the CN); this
// middleware decides WHAT that CN may request.
//
// The acl maps each trusted CN to the set of platforms it may query. A value of
// "*" in a CN's list grants access to any platform (used for the orchestrator
// and the api internal-admin identity). A CN absent from the map is rejected.
//
// Behavior:
//   - With a peer cert, the Subject.CommonName is the caller identity.
//   - Without a peer cert: when pkg/mtls.IsEnabled() reports true (production),
//     reject 403 — the listener should never have accepted this connection, but
//     the middleware is defense in depth. When IsEnabled() reports false (unit
//     test runs without cert material), the identity becomes "system" and must
//     still appear in the ACL map to proceed (fail-closed).
//   - The CN must be present in acl, else 403 "service not authorized".
//   - The platform query param must be permitted for that CN (exact match or a
//     "*" wildcard entry), else 403 "platform not authorized for this service".
//
// On allow, the resolved CN is stored on r.Context() under the same
// ctxKeyServiceIdentity used by RequireServiceIdentity, so downstream handlers
// (and the SEC-04 token-decrypt audit) read it via ServiceIdentityFromContext.
//
// The supplied logger is used for warn-level traces on reject paths. Nil falls
// back to slog.Default().
func RequirePlatformACL(acl map[string][]string, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var cn string
			switch {
			case r.TLS != nil && len(r.TLS.PeerCertificates) > 0:
				cn = r.TLS.PeerCertificates[0].Subject.CommonName
			default:
				if mtls.IsEnabled() {
					log.WarnContext(r.Context(), "internal: mtls required but no peer cert",
						"path", r.URL.Path)
					http.Error(w, "mtls required", http.StatusForbidden)
					return
				}
				cn = "system"
			}
			allowed, ok := acl[cn]
			if !ok {
				log.WarnContext(r.Context(), "internal: unknown service CN",
					"cn", cn, "path", r.URL.Path)
				http.Error(w, "service not authorized", http.StatusForbidden)
				return
			}
			platform := r.URL.Query().Get("platform")
			permitted := false
			for _, p := range allowed {
				if p == "*" || p == platform {
					permitted = true
					break
				}
			}
			if !permitted {
				log.WarnContext(r.Context(), "internal: platform not in CN ACL",
					"cn", cn, "platform", platform, "path", r.URL.Path)
				http.Error(w, "platform not authorized for this service", http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyServiceIdentity{}, cn)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
