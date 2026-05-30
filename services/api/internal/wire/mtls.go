package wire

import (
	"crypto/tls"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

// MaybeServerTLSConfig returns the *tls.Config the API's internal :8443
// listener should use, or nil when mTLS is disabled.
//
// Behavior:
//   - ONEVOICE_MTLS_ENABLED=true → reads CA + leaf cert/key paths from
//     env, returns a server config with ClientAuth=RequireAndVerifyClientCert.
//     Any path missing / unreadable becomes a startup error so the operator
//     surfaces a misconfig BEFORE the listener silently falls back to plain
//     HTTP.
//   - Anything else → returns (nil, nil). Caller calls ListenAndServe (plain HTTP).
//     Tests + dev runs without certs continue working.
//
// This is the single bring-up point for server-side mTLS. cmd/main.go
// branches on the nil return; pkg/tokenclient has the symmetric client-side
// branch.
func MaybeServerTLSConfig() (*tls.Config, error) {
	if !mtls.IsEnabled() {
		return nil, nil
	}
	paths, err := mtls.PathsFromEnv()
	if err != nil {
		return nil, fmt.Errorf("internal listener: %w", err)
	}
	cfg, err := mtls.LoadServerTLSConfig(paths)
	if err != nil {
		return nil, fmt.Errorf("internal listener: %w", err)
	}
	return cfg, nil
}
