// Package natsauth builds the NATS client security options (authentication and
// TLS) that every OneVoice service applies when it dials NATS. Credentials and
// TLS material are read from the environment, so the same binary runs
// unauthenticated in local dev and authenticated + TLS-wrapped in production
// without a code change.
package natsauth

import (
	"os"

	natslib "github.com/nats-io/nats.go"
)

// Options returns the NATS dial options derived from the environment. Every
// source is optional; with none set the returned slice is empty and the caller
// dials exactly as before:
//
//   - NATS_CREDS: path to a NATS credentials (JWT+nkey) file. Takes precedence
//     over NATS_USER/NATS_PASSWORD when both are set.
//   - NATS_USER / NATS_PASSWORD: static username/password authentication.
//   - NATS_CA_PATH: PEM bundle used to verify the NATS server certificate (TLS).
//   - NATS_CLIENT_CERT / NATS_CLIENT_KEY: client certificate for mutual TLS.
//
// File-backed options (credentials, CA bundle, client cert) defer their reads
// to Connect time, so a missing or malformed file surfaces as a connect error
// rather than a panic here.
func Options() []natslib.Option {
	var opts []natslib.Option

	if creds := os.Getenv("NATS_CREDS"); creds != "" {
		opts = append(opts, natslib.UserCredentials(creds))
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
