// Package natsauth builds the NATS client security options (authentication and
// TLS) that every OneVoice service applies when it dials NATS. Credentials and
// TLS material are read from the environment, so the same binary runs
// unauthenticated in local dev and authenticated + TLS-wrapped in production
// without a code change.
package natsauth

import (
	"fmt"
	"os"

	natslib "github.com/nats-io/nats.go"
)

// Options returns the NATS dial options derived from the environment. Every
// source is optional; with none set the returned slice is empty and the caller
// dials exactly as before. Authentication sources are tried in precedence order
// (first non-empty wins):
//
//   - NATS_CREDS: path to a NATS credentials (JWT+nkey) file.
//   - NATS_NKEY_SEED: path to a file holding a single nkey user seed
//     (`SU...`). The seed is the service's cryptographic identity; it signs the
//     server's nonce and never crosses the wire. This is the production auth
//     mode — each service presents its own seed and the server maps the derived
//     public key to a subject-scoped permission set (see infra/nats).
//   - NATS_USER / NATS_PASSWORD: static username/password authentication
//     (kept for simple/local setups).
//
// TLS material is applied independently of the auth source:
//
//   - NATS_CA_PATH: PEM bundle used to verify the NATS server certificate (TLS).
//   - NATS_CLIENT_CERT / NATS_CLIENT_KEY: client certificate for mutual TLS.
//
// File-backed options validate their material at Connect time. Missing or
// malformed seeds fail before dialing, with the credential path in the error.
func Options() []natslib.Option {
	var opts []natslib.Option

	if creds := os.Getenv("NATS_CREDS"); creds != "" {
		opts = append(opts, natslib.UserCredentials(creds))
	} else if seed := os.Getenv("NATS_NKEY_SEED"); seed != "" {
		opts = append(opts, func(options *natslib.Options) error {
			opt, err := natslib.NkeyOptionFromSeed(seed)
			if err != nil {
				return fmt.Errorf("load NATS nkey seed %q: %w", seed, err)
			}
			return opt(options)
		})
	} else if user := os.Getenv("NATS_USER"); user != "" {
		opts = append(opts, natslib.UserInfo(user, os.Getenv("NATS_PASSWORD")))
	}

	if caPath := os.Getenv("NATS_CA_PATH"); caPath != "" {
		opts = append(opts, natslib.RootCAs(caPath))
	}
	if cert, key := os.Getenv("NATS_CLIENT_CERT"), os.Getenv("NATS_CLIENT_KEY"); cert != "" && key != "" {
		opts = append(opts, natslib.ClientCert(cert, key))
	}

	return opts
}
