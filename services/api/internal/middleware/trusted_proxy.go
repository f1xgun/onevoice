package middleware

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Trusted-proxy ClientIP helper.
//
// The default chi RealIP middleware trusts X-Forwarded-For unconditionally,
// which lets an attacker spoof an IP and either (a) pin the lockout counter
// to someone else's /16 to DoS them or (b) escape their own /16 to avoid
// being rate-limited. We CANNOT remove RealIP (other middleware depends on
// r.RemoteAddr being the upstream IP) but the lockout-key derivation MUST
// use this stricter ClientIP() instead.
//
// Behavior: when the connection's peer IP (split off r.RemoteAddr) is in a
// TRUSTED_PROXY_CIDRS-allowlisted CIDR, the leftmost X-Forwarded-For entry
// is returned. Otherwise the peer IP is returned and any client-supplied
// XFF header is ignored. This is the "safer-side default" — misconfigured
// CIDR list never escalates trust.

// ipv6BucketBits is the IPv6 prefix length used for the lockout bucket. /64
// is the standard single-customer allocation, mirroring the IPv4 /16 intent.
const (
	ipv6BucketBits = 64
	ipv6AddrBits   = 128
)

var (
	trustedCIDRs []*net.IPNet
	trustedMu    sync.RWMutex
)

// Yandex Cloud Network Load Balancer published IP ranges. Verified
// 2026-05 (planning time). Operator overrides via TRUSTED_PROXY_CIDRS env;
// refresh with `yc managed-network-load-balancer list-ip-addresses` when
// Yandex publishes new ranges.
var defaultTrustedCIDRs = []string{
	"178.154.250.0/24",
	"84.252.160.0/19",
}

// InitTrustedProxies parses a comma-separated CIDR list once at startup
// and installs it as the trust set for ClientIP. An empty input falls
// back to defaultTrustedCIDRs so an operator who forgets to set the env
// var still gets Yandex Cloud LB coverage by default.
//
// Returns an error on any unparseable CIDR — startup should fail fast
// rather than silently degrade to "trust nothing".
func InitTrustedProxies(csv string) error {
	csv = strings.TrimSpace(csv)
	list := defaultTrustedCIDRs
	if csv != "" {
		list = strings.Split(csv, ",")
	}
	parsed := make([]*net.IPNet, 0, len(list))
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(raw)
		if err != nil {
			return fmt.Errorf("trusted_proxy: invalid CIDR %q: %w", raw, err)
		}
		parsed = append(parsed, cidr)
	}
	trustedMu.Lock()
	defer trustedMu.Unlock()
	trustedCIDRs = parsed
	slog.Info("trusted_proxy: initialized", "cidr_count", len(parsed))
	return nil
}

// ClientIP returns the canonical client IP for lockout/captcha keying.
//
// Algorithm:
//  1. Parse the peer IP out of r.RemoteAddr (strip port; handle IPv6 brackets).
//  2. If the peer IP is in any trusted CIDR, return the leftmost
//     X-Forwarded-For entry — the upstream LB has vouched for it.
//  3. Otherwise return the peer IP and IGNORE X-Forwarded-For. Client-set
//     XFF from a non-LB peer is attacker-controlled.
//
// Falls back to r.RemoteAddr unchanged when SplitHostPort fails or the
// host is not a parseable IP — preserves forensic value over correctness.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return host
	}
	trustedMu.RLock()
	cidrs := trustedCIDRs
	trustedMu.RUnlock()
	for _, c := range cidrs {
		if c.Contains(peer) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if idx := strings.IndexByte(xff, ','); idx > 0 {
					return strings.TrimSpace(xff[:idx])
				}
				return strings.TrimSpace(xff)
			}
			return host
		}
	}
	return host
}

// Net16 returns the coarse network bucket used to key the account lockout
// counter. For IPv4 it is the /16 prefix ("x.y.0.0/16"); for IPv6 it is the
// /64 prefix ("<masked>/64"). Returns "" only for genuinely unparseable input
// (logged at warn — caller should treat "" as "skip lockout for this request"
// per the middleware fall-through pattern).
//
// The bucket blunts shared-NAT/rotation collisions while still binding to the
// network neighborhood. A /32 (IPv4) or /128 (IPv6) would let an attacker on a
// mobile carrier hop addresses and avoid the counter; an over-broad prefix
// would let one bad neighbor lock out unrelated customers. /64 is the standard
// single-customer IPv6 allocation, mirroring the IPv4 /16 intent.
func Net16(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		slog.Warn("trusted_proxy: Net16 called on unparseable IP", "ip", ip)
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.0.0/16", v4[0], v4[1])
	}
	masked := parsed.Mask(net.CIDRMask(ipv6BucketBits, ipv6AddrBits))
	return fmt.Sprintf("%s/%d", masked.String(), ipv6BucketBits)
}
