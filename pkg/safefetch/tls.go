package safefetch

import "crypto/tls"

// newInsecureTLSConfig returns a TLS config that skips certificate
// verification. SSRF protection is independent of TLS trust: the dial-time IP
// disallow-list still runs, so a caller that opts into InsecureSkipVerify for
// self-signed external image hosts does not weaken the network-policy guard.
func newInsecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // G402: external image hosts may use self-signed certs; the SSRF IP screen is independent of TLS verification
}
