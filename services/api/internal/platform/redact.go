package platform

import (
	"errors"
	"net/url"
)

// redactURLErr scrubs secrets from a *url.Error before it is logged. On a
// transport failure net/http returns a *url.Error whose .Error() embeds the
// FULL request URL. The platform syncers carry credentials in that URL —
// VK puts access_token in the query, the Telegram Bot API puts the bot token
// in the path (/bot<token>/method) — so both the query and the path are
// blanked. Blanking the path is harmless for VK and required for Telegram.
// Non-*url.Error inputs are returned unchanged.
func redactURLErr(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	if u, parseErr := url.Parse(ue.URL); parseErr == nil {
		u.Path = "/REDACTED"
		u.RawQuery = "REDACTED"
		ue.URL = u.String()
	}
	return err
}
