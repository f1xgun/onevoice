package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

func TestClientIP_RemoteAddrUntrustedIgnoresXFF(t *testing.T) {
	// Init with a deliberately-narrow trust set; 1.2.3.4 is OUTSIDE it.
	// The XFF header must be ignored — returns the peer IP.
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "1.2.3.4:12345"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")

	assert.Equal(t, "1.2.3.4", middleware.ClientIP(r))
}

func TestClientIP_RemoteAddrTrustedReadsXFF(t *testing.T) {
	// Peer is INSIDE the trust set → trust the LB's XFF. Leftmost wins.
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:12345"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 8.8.8.8")

	assert.Equal(t, "9.9.9.9", middleware.ClientIP(r))
}

func TestClientIP_TrustedNoXFF_FallsBackToPeer(t *testing.T) {
	// Edge case: LB peer but no XFF header → return the LB IP itself
	// rather than empty string. Forensic value preserved.
	require.NoError(t, middleware.InitTrustedProxies("10.0.0.0/8"))

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:12345"

	assert.Equal(t, "10.0.0.5", middleware.ClientIP(r))
}

func TestClientIP_DefaultCIDRs_WhenEmptyInput(t *testing.T) {
	// Empty CSV → defaults to Yandex Cloud LB ranges. 178.154.250.5 is
	// inside 178.154.250.0/24 (the published Yandex Cloud LB range), so
	// the XFF must be honored.
	require.NoError(t, middleware.InitTrustedProxies(""))

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "178.154.250.5:443"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	assert.Equal(t, "203.0.113.7", middleware.ClientIP(r))
}

func TestInitTrustedProxies_InvalidCIDR_ReturnsError(t *testing.T) {
	// Fail-fast on operator typo — never silently degrade to "trust nothing".
	err := middleware.InitTrustedProxies("not-a-cidr,10.0.0.0/8")
	require.Error(t, err)
}

func TestNet16_IPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"1.2.3.4", "1.2.0.0/16"},
		{"10.0.5.6", "10.0.0.0/16"},
		{"192.168.100.200", "192.168.0.0/16"},
		{"0.0.0.0", "0.0.0.0/16"},
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			assert.Equal(t, tc.want, middleware.Net16(tc.ip))
		})
	}
}

func TestNet16_IPv6_ReturnsEmpty(t *testing.T) {
	// IPv6 bucketing TBD; for now Net16 returns "" and the lockout
	// middleware treats that as "skip lockout for this request".
	assert.Equal(t, "", middleware.Net16("fe80::1"))
	assert.Equal(t, "", middleware.Net16("::1"))
}

func TestNet16_Unparseable_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", middleware.Net16("not-an-ip"))
	assert.Equal(t, "", middleware.Net16(""))
}
