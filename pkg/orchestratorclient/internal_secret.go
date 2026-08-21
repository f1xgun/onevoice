package orchestratorclient

import "net/http"

// InternalSecretHeader carries the shared service-to-service secret that
// authenticates api→orchestrator calls to the orchestrator's cluster-internal
// inbound (chat stream, HITL resume, tool registry, draft-reply). The
// orchestrator rejects any request whose header does not match its configured
// ORCHESTRATOR_INTERNAL_SECRET. Defined here as the single source of truth so
// the api-side injector and the orchestrator-side middleware cannot drift on
// the header name.
const InternalSecretHeader = "X-Internal-Secret" //nolint:gosec // header name, not a credential

// InternalSecretMinLen is the minimum accepted length of
// ORCHESTRATOR_INTERNAL_SECRET on both the api (sender) and orchestrator
// (verifier). Shared so both sides reject the same weak values. Generate with
// `openssl rand -base64 32`.
const InternalSecretMinLen = 16

// secretRoundTripper injects InternalSecretHeader on every outbound request
// before delegating to base. It clones the request so it never mutates the
// caller's *http.Request, per the http.RoundTripper contract.
type secretRoundTripper struct {
	base   http.RoundTripper
	secret string
}

// RoundTrip stamps the shared secret header and forwards to the base transport.
func (t *secretRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set(InternalSecretHeader, t.secret)
	return t.base.RoundTrip(clone)
}

// WithInternalSecret returns an *http.Client whose transport stamps
// InternalSecretHeader=secret on every request, so all orchestratorclient
// calls made through it authenticate to the orchestrator's internal inbound.
//
// A nil base is treated as a fresh &http.Client{} (default transport). An empty
// secret returns the base unchanged (nil → http.DefaultClient), which keeps the
// dev/test path — where the orchestrator middleware is also disabled — working
// without a secret. The base client's timeout and other fields are preserved.
func WithInternalSecret(base *http.Client, secret string) *http.Client {
	if secret == "" {
		if base == nil {
			return http.DefaultClient
		}
		return base
	}
	c := &http.Client{}
	if base != nil {
		*c = *base
	}
	inner := c.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	c.Transport = &secretRoundTripper{base: inner, secret: secret}
	return c
}
