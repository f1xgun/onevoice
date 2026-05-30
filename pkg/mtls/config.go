// Package mtls is the single source of truth for cluster-internal TLS
// material loading on OneVoice. It is consumed by:
//
//   - services/api's internal :8443 listener (server side — RequireAndVerifyClientCert)
//   - pkg/tokenclient + future pkg/billingclient (client side — presents
//     a service cert, trusts only the dev CA)
//
// Both sides read the same three env vars:
//
//	ONEVOICE_MTLS_CA_PATH    — PEM root CA bundle (same for every service)
//	ONEVOICE_MTLS_CERT_PATH  — service-specific leaf cert PEM
//	ONEVOICE_MTLS_KEY_PATH   — service-specific private key PEM
//
// plus a feature toggle:
//
//	ONEVOICE_MTLS_ENABLED    — "true" enforces mTLS; anything else is a no-op
//
// The toggle exists so unit tests that spin up plain httptest.NewServer
// (no certs in the process env) can continue to compile and pass without
// shelling out to openssl. Production / CI sets the toggle to "true"; the
// loader returns an error when any path env var is empty AND the toggle
// is on, so a misconfigured deploy fails fast rather than silently falling
// back to plain HTTP (which would be the worst-of-both-worlds bug).
package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Env var names — exported as constants so callers can reference them in
// log messages without string drift.
const (
	EnvEnabled  = "ONEVOICE_MTLS_ENABLED"
	EnvCAPath   = "ONEVOICE_MTLS_CA_PATH"
	EnvCertPath = "ONEVOICE_MTLS_CERT_PATH"
	EnvKeyPath  = "ONEVOICE_MTLS_KEY_PATH"
)

// ServiceCertPaths bundles the three filesystem paths needed to construct
// a *tls.Config for either server- or client-side mTLS.
//
// All three fields are required when ONEVOICE_MTLS_ENABLED=true.
type ServiceCertPaths struct {
	// CACertPath is the PEM root CA used to verify the OTHER party's cert.
	// Server side: ClientCAs verifier. Client side: RootCAs verifier.
	CACertPath string
	// CertPath is THIS service's leaf cert PEM.
	CertPath string
	// KeyPath is THIS service's private key PEM (matches CertPath).
	KeyPath string
}

// PathsFromEnv reads ONEVOICE_MTLS_CA_PATH / ONEVOICE_MTLS_CERT_PATH /
// ONEVOICE_MTLS_KEY_PATH from the process env and returns them as a
// ServiceCertPaths.
//
// When ONEVOICE_MTLS_ENABLED=true, any missing env var causes an error so
// the caller can refuse to start. When the toggle is false (or unset), the
// returned struct may contain empty strings — the caller is expected to
// branch on IsEnabled() before attempting to load the certs.
//
// The function intentionally takes NO arguments. Each service identifies
// itself via the per-container env vars set in docker-compose.yml; there is
// no service-name parameter to drift.
func PathsFromEnv() (ServiceCertPaths, error) {
	p := ServiceCertPaths{
		CACertPath: os.Getenv(EnvCAPath),
		CertPath:   os.Getenv(EnvCertPath),
		KeyPath:    os.Getenv(EnvKeyPath),
	}
	if !IsEnabled() {
		return p, nil
	}
	var missing []string
	if p.CACertPath == "" {
		missing = append(missing, EnvCAPath)
	}
	if p.CertPath == "" {
		missing = append(missing, EnvCertPath)
	}
	if p.KeyPath == "" {
		missing = append(missing, EnvKeyPath)
	}
	if len(missing) > 0 {
		return p, fmt.Errorf("mtls: ONEVOICE_MTLS_ENABLED=true but missing env var(s): %v", missing)
	}
	return p, nil
}

// IsEnabled reports whether the operator opted into mTLS. Used as a quick
// branch by callers (e.g. tokenclient) that have a back-compat plain-HTTP
// fallback path for tests.
func IsEnabled() bool {
	return os.Getenv(EnvEnabled) == "true"
}

// LoadServerTLSConfig produces a *tls.Config wired for an mTLS-terminating
// HTTPS listener. Suitable for http.Server.TLSConfig + ListenAndServeTLS("","").
//
// Specifically:
//   - Certificates  — THIS service's leaf cert (so ListenAndServeTLS("","")
//     works without re-reading the cert/key files).
//   - ClientCAs     — the dev CA root, used to verify the peer's client cert.
//   - ClientAuth    — RequireAndVerifyClientCert: the listener REJECTS the
//     handshake unless the client presents a cert signed by ClientCAs.
//   - MinVersion    — TLS 1.2 (matches the threat model T-25a-02 mitigation).
func LoadServerTLSConfig(p ServiceCertPaths) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(p.CertPath, p.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("mtls: load server keypair (%s, %s): %w", p.CertPath, p.KeyPath, err)
	}
	caPool, err := loadCAPool(p.CACertPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// LoadClientTLSConfig produces a *tls.Config suitable for
// http.Transport.TLSClientConfig — i.e. the calling service presents its own
// leaf cert to the API's internal listener and trusts only the dev CA.
//
// Mirror of LoadServerTLSConfig but with RootCAs instead of ClientCAs (and
// no ClientAuth — that's a server-side concept).
func LoadClientTLSConfig(p ServiceCertPaths) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(p.CertPath, p.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("mtls: load client keypair (%s, %s): %w", p.CertPath, p.KeyPath, err)
	}
	caPool, err := loadCAPool(p.CACertPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// loadCAPool reads a PEM CA bundle from disk and returns it as an
// *x509.CertPool ready for tls.Config.ClientCAs / RootCAs. Returns a wrapped
// fs error when the file is missing — callers must NOT panic.
func loadCAPool(caPath string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("mtls: read CA bundle (%s): %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("mtls: CA bundle at %s contained no parsable certificates", caPath)
	}
	return pool, nil
}
