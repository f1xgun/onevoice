// Package netdial provides a net/http dialer that forces outbound TCP onto
// IPv4.
//
// Some hosts have no working IPv6 route — notably Yandex Cloud VMs — yet
// dual-stack hostnames such as api.telegram.org publish AAAA (IPv6) records.
// Go's default dialer may pick the v6 address first, and every request then
// hangs until its timeout. Routing dials through TCP4DialContext forces the A
// record and sidesteps the dead v6 path.
package netdial

import (
	"context"
	"net"
	"time"
)

const (
	dialTimeout   = 10 * time.Second
	dialKeepAlive = 30 * time.Second
)

// TCP4DialContext is an http.Transport DialContext that ignores the requested
// network and always dials over "tcp4". Assign it to a Transport:
//
//	&http.Transport{DialContext: netdial.TCP4DialContext}
func TCP4DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}
	return d.DialContext(ctx, "tcp4", addr)
}
