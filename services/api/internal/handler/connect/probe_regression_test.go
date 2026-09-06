package connect

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/netdial"
)

type probeRoundTripper func(*http.Request) (*http.Response, error)

func (f probeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConnectClientPinsIPv4(t *testing.T) {
	for _, supplied := range []*http.Client{nil, {Timeout: 10 * time.Second}} {
		t.Run(fmtClientName(supplied), func(t *testing.T) {
			h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, supplied)
			transport, ok := h.httpClient.Transport.(*http.Transport)
			require.True(t, ok)
			require.Equal(t, reflect.ValueOf(netdial.TCP4DialContext).Pointer(), reflect.ValueOf(transport.DialContext).Pointer())
			require.Equal(t, 10*time.Second, h.httpClient.Timeout)
			if supplied != nil {
				require.Nil(t, supplied.Transport)
			}
		})
	}
}

func fmtClientName(c *http.Client) string {
	if c == nil {
		return "default"
	}
	return "production supplied client"
}

func TestProbeTelegramLinkedGroupClassification(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		err              error
		id               int64
	}{
		{name: "no group", want: "no_linked_group"},
		{name: "member", id: -1001, body: `{"ok":true,"result":{"id":-1001}}`, want: "ok"},
		{name: "forbidden", id: -1001, body: `{"ok":false,"error_code":403,"description":"Forbidden: bot is not a member"}`, want: "bot_not_member"},
		{name: "rate limited", id: -1001, body: `{"ok":false,"error_code":429,"description":"Too Many Requests"}`, want: "unknown"},
		{name: "transport", id: -1001, err: errors.New("connection unavailable"), want: "unknown"},
		{name: "parse", id: -1001, body: `invalid`, want: "unknown"},
		{name: "rejected", id: -1001, body: `{"ok":false,"error_code":400,"description":"Bad Request"}`, want: "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: probeRoundTripper(func(*http.Request) (*http.Response, error) {
				require.NotZero(t, tt.id)
				if tt.err != nil {
					return nil, tt.err
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
			})}
			h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, client)
			require.Same(t, client, h.httpClient)
			require.Equal(t, tt.want, h.probeTelegramLinkedGroup(context.Background(), "test", tt.id))
		})
	}
}
