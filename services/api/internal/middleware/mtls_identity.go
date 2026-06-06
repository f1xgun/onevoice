package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

// ctxKeyServiceIdentity is the private context key under which
// RequireServiceIdentity stores the resolved per-request service identity
// (the client cert CommonName or "system" in test mode).
type ctxKeyServiceIdentity struct{}

// RequireServiceIdentity gates internal :8443 routes by the client cert's
// CommonName. The mTLS substrate (pkg/mtls) already verifies the cert chain
// at the listener; this middleware applies the per-route allowlist on top
// of that.
//
// Behavior:
//   - If a peer cert is present, the Subject.CommonName must appear in
//     `allowlist` (case-sensitive — defense against typos). Reject 403 if not.
//   - If NO peer cert is present (r.TLS == nil OR PeerCertificates empty):
//   - When pkg/mtls.IsEnabled() reports true (production), reject 403 —
//     the listener should never have accepted this connection, but the
//     middleware is defense in depth in case of misconfiguration.
//   - When pkg/mtls.IsEnabled() reports false (unit test runs without
//     cert material), set the identity to "system" and continue. This
//     preserves the existing test-loop ergonomics.
//
// On allow, the resolved identity is stored on r.Context() under
// ctxKeyServiceIdentity; handlers retrieve it via ServiceIdentityFromContext.
//
// The supplied logger is used for warn-level traces on reject paths. Nil is
// tolerated — the middleware falls back to slog.Default().
func RequireServiceIdentity(allowlist []string, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var identity string
			switch {
			case r.TLS != nil && len(r.TLS.PeerCertificates) > 0:
				identity = r.TLS.PeerCertificates[0].Subject.CommonName
				if !slices.Contains(allowlist, identity) {
					log.WarnContext(r.Context(), "internal: service identity rejected",
						"cn", identity, "path", r.URL.Path)
					http.Error(w, "service not authorized", http.StatusForbidden)
					return
				}
			default:
				if mtls.IsEnabled() {
					log.WarnContext(r.Context(), "internal: mtls required but no peer cert",
						"path", r.URL.Path)
					http.Error(w, "mtls required", http.StatusForbidden)
					return
				}
				identity = "system"
			}
			ctx := context.WithValue(r.Context(), ctxKeyServiceIdentity{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ServiceIdentityFromContext returns the per-request service identity stored
// by RequireServiceIdentity. Returns "" when the middleware did not run
// (e.g. routes not gated by it).
func ServiceIdentityFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyServiceIdentity{}).(string)
	return v
}

// WithServiceIdentity stores a service identity on ctx under the same private
// key RequireServiceIdentity uses. It is the exported counterpart to
// ServiceIdentityFromContext for callers that resolve identity outside the
// middleware chain (e.g. in-process callers or tests).
func WithServiceIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, ctxKeyServiceIdentity{}, identity)
}
