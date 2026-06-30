package agentbase

import "net/url"

// redactConnURL returns a log-safe form of a connection URL (redis:// or
// nats://) that never carries an embedded userinfo password. It parses raw and
// returns url.Redacted (which masks the password as "xxxxx"); if raw is empty or
// fails to parse it returns "<redacted>" rather than risk emitting credentials.
func redactConnURL(raw string) string {
	if raw == "" {
		return "<redacted>"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "<redacted>"
	}
	return u.Redacted()
}
