// Package safefetch downloads caller-supplied (and therefore untrusted) URLs
// while defending against server-side request forgery (SSRF).
//
// Platform agents accept a photo URL as an LLM tool argument and fetch it to
// re-upload the bytes. Because that argument is prompt-injectable, a naive
// http.Get lets an attacker steer the agent at internal addresses (cloud
// metadata, loopback, RFC1918, CGNAT) for port scanning, metadata theft, or
// GET-triggered side effects. Fetch enforces an https-only scheme, screens
// both URL-literal and DNS-resolved peer IPs against a disallow-list, forces
// every redirect hop through the same policy, caps the redirect chain, and
// bounds the response body. The dial-time IP screen runs after DNS resolution
// but before the handshake, which also defeats DNS rebinding.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// ErrUnsafeURL is returned when a caller-supplied URL points at a non-https
// endpoint or an internal/loopback/link-local/private network. Callers should
// treat it as a validation (policy) error, not a transient network failure.
var ErrUnsafeURL = errors.New("unsafe fetch url")

const (
	// DefaultMaxBytes caps a downloaded body. A hostile or oversized response
	// cannot exhaust the worker's memory beyond this bound.
	DefaultMaxBytes int64 = 10 << 20 // 10 MiB

	// DefaultTimeout is the total budget for a fetch, including redirects.
	DefaultTimeout = 15 * time.Second

	// DefaultMaxRedirects caps the redirect chain length.
	DefaultMaxRedirects = 5
)

// cgnatRange is RFC 6598 shared address space (100.64.0.0/10). Go's
// net.IP.IsPrivate predates RFC 6598 and excludes it, but NAT/cloud
// environments route internal services through this range, so it must be
// screened alongside RFC1918.
var cgnatRange = mustCIDR("100.64.0.0/10")

// thisHostRange is the RFC 1122 "this host on this network" block
// (0.0.0.0/8). net.IP.IsUnspecified only matches 0.0.0.0 exactly, but on
// Linux any 0.x.x.x address routes to loopback, so e.g. https://0.0.0.1/ is a
// real SSRF-to-localhost vector and the whole /8 must be screened.
var thisHostRange = mustCIDR("0.0.0.0/8")

// mustCIDR parses a static CIDR at package-init; it panics on a malformed
// constant, which is a programming error, not runtime input.
func mustCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		panic("safefetch: invalid CIDR " + s + ": " + err.Error())
	}
	return network
}

// isDisallowedIP reports whether ip belongs to a range the worker must never
// connect to: loopback, link-local (incl. the cloud metadata endpoint),
// private (RFC1918 / RFC4193), CGNAT shared space (RFC 6598), the 0.0.0.0/8
// this-host block (loopback-routed on Linux), unspecified, or multicast. It is
// the single source of truth for both URL-literal screening and connect-time
// screening.
func isDisallowedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		ip.IsPrivate() ||
		cgnatRange.Contains(ip) ||
		thisHostRange.Contains(ip)
}

// ValidateURL parses rawURL and enforces the scheme/host policy for an
// untrusted fetch source: it must be a well-formed https URL with a host. Any
// IP literal embedded in the host is additionally screened with isDisallowedIP;
// DNS hostnames are screened later at connect time by the dial Control hook,
// which also defeats DNS rebinding. It returns ErrUnsafeURL (wrapped) on any
// policy violation so callers can distinguish a blocked URL from a network
// error.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parse: %v", ErrUnsafeURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q is not https", ErrUnsafeURL, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrUnsafeURL)
	}
	if ip := net.ParseIP(host); ip != nil && isDisallowedIP(ip) {
		return fmt.Errorf("%w: host %q is a disallowed address", ErrUnsafeURL, host)
	}
	return nil
}

// safeControl is a net.Dialer Control hook that rejects the connection if the
// already-resolved peer address falls in a disallowed range. Because it runs
// after DNS resolution but before the handshake, it screens the actual IP the
// socket would connect to and therefore defeats DNS rebinding.
func safeControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: bad dial address %q", ErrUnsafeURL, address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: unresolved dial host %q", ErrUnsafeURL, host)
	}
	if isDisallowedIP(ip) {
		return fmt.Errorf("%w: refusing to connect to %s", ErrUnsafeURL, host)
	}
	return nil
}

// Options tunes a Fetcher. The zero value is valid and falls back to the
// Default* constants.
type Options struct {
	// Timeout is the total per-request budget, including redirects. Zero uses
	// DefaultTimeout.
	Timeout time.Duration
	// MaxBytes caps the response body. Zero uses DefaultMaxBytes.
	MaxBytes int64
	// MaxRedirects caps the redirect chain. Zero uses DefaultMaxRedirects.
	MaxRedirects int
	// ForceIPv4 routes dials over "tcp4". Some hosts (e.g. Yandex Cloud VMs)
	// have no IPv6 route, so leaving the default network can hang on AAAA
	// records until timeout.
	ForceIPv4 bool
}

// Fetcher performs SSRF-safe HTTPS fetches. Construct it with New and reuse it;
// it is safe for concurrent use.
type Fetcher struct {
	client   *http.Client
	maxBytes int64
}

// New builds a Fetcher whose http.Client screens every TCP connection through
// the disallow-list, re-validates each redirect target against the same
// scheme/host policy, caps the redirect chain, and applies the timeout.
func New(opts Options) *Fetcher {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	maxRedirects := opts.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = DefaultMaxRedirects
	}

	dialer := &net.Dialer{Control: safeControl}
	network := "tcp"
	if opts.ForceIPv4 {
		network = "tcp4"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(reqRedirect *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrUnsafeURL)
			}
			return ValidateURL(reqRedirect.URL.String())
		},
	}
	return &Fetcher{client: client, maxBytes: maxBytes}
}

// Get validates rawURL, dials only public addresses, and returns the response
// body capped at the Fetcher's MaxBytes. A body larger than the cap is an
// error rather than a silent truncation. The contentType is the raw
// Content-Type header so callers can keep their own image/* checks.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (body []byte, contentType string, err error) {
	if vErr := ValidateURL(rawURL); vErr != nil {
		return nil, "", vErr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}

	read, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(read)) > f.maxBytes {
		return nil, "", fmt.Errorf("%s exceeds %d bytes", rawURL, f.maxBytes)
	}
	return read, resp.Header.Get("Content-Type"), nil
}
