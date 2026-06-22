package yandex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
)

// ErrUnsafeURL is returned when a caller-supplied photo URL points at a
// non-https endpoint or an internal/loopback/link-local/private network.
var ErrUnsafeURL = errors.New("unsafe photo url")

// validatePhotoURL parses rawURL and enforces the scheme/host policy for a
// caller-supplied photo source: it must be a well-formed https URL with a host.
// It is a pure function so the policy is unit-testable in isolation. Any IP
// literal embedded in the host is additionally screened with isDisallowedIP;
// DNS hostnames are screened later at connect time by the dial Control hook,
// which also defeats DNS rebinding.
func validatePhotoURL(rawURL string) error {
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
		return fmt.Errorf("%w: host %q resolves to a disallowed address", ErrUnsafeURL, host)
	}
	return nil
}

// isDisallowedIP reports whether ip belongs to a range the RPA worker must
// never connect to: loopback, link-local (incl. the cloud metadata endpoint),
// private (RFC1918 / RFC4193), unspecified, or multicast. It is the single
// source of truth for both URL-literal screening and connect-time screening.
func isDisallowedIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		ip.IsPrivate() {
		return true
	}
	return false
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

// safePhotoClient builds an http.Client that screens every TCP connection it
// makes through safeControl and re-validates each redirect target against the
// same scheme/host policy, capping the redirect chain. The dialer forces IPv4
// to match the rest of the agent's outbound traffic.
func safePhotoClient() *http.Client {
	dialer := &net.Dialer{Control: safeControl}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
	}
	return &http.Client{
		Timeout:   photoFetchTimeout,
		Transport: transport,
		CheckRedirect: func(reqRedirect *http.Request, via []*http.Request) error {
			if len(via) >= maxPhotoRedirects {
				return fmt.Errorf("%w: too many redirects", ErrUnsafeURL)
			}
			return validatePhotoURL(reqRedirect.URL.String())
		},
	}
}

// fetchPhoto downloads a caller-supplied photo after validating its URL and
// connecting only to public addresses. The body is capped at maxPhotoBytes so a
// hostile or oversized response cannot exhaust the worker's memory.
func fetchPhoto(ctx context.Context, photoURL string) ([]byte, error) {
	if err := validatePhotoURL(photoURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build photo request: %w", err)
	}

	resp, err := safePhotoClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download photo from %s: %w", photoURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download photo from %s: status %d", photoURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read photo body: %w", err)
	}
	if int64(len(body)) > maxPhotoBytes {
		return nil, fmt.Errorf("photo from %s exceeds %d bytes", photoURL, maxPhotoBytes)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("downloaded empty file from %s", photoURL)
	}
	return body, nil
}
