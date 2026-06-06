package yandex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// injectCookies parses a JSON cookie array and adds it to the browser context.
func injectCookies(bCtx playwright.BrowserContext, cookiesJSON string) error {
	var cookies []map[string]interface{}
	if err := json.Unmarshal([]byte(cookiesJSON), &cookies); err != nil {
		return fmt.Errorf("parse cookies JSON: %w", err)
	}
	pwCookies := make([]playwright.OptionalCookie, 0, len(cookies))
	for _, c := range cookies {
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		domain, _ := c["domain"].(string)
		path, _ := c["path"].(string)
		pwCookies = append(pwCookies, playwright.OptionalCookie{
			Name:   name,
			Value:  value,
			Domain: playwright.String(domain),
			Path:   playwright.String(path),
		})
	}
	return bCtx.AddCookies(pwCookies)
}

// exchangeOAuthForSession uses Yandex's /am/cookie endpoint to convert an OAuth
// access token into browser session cookies, then injects them into the context.
func exchangeOAuthForSession(bCtx playwright.BrowserContext, oauthToken string) error {
	page, err := bCtx.NewPage()
	if err != nil {
		return fmt.Errorf("new page for oauth exchange: %w", err)
	}
	defer func() { _ = page.Close() }()

	authURL := yandexPassportAuthURL
	_, _ = page.Goto(authURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(tabSwitchTimeoutMs),
	})

	script := fmt.Sprintf(`async () => {
		try {
			const resp = await fetch('https://passport.yandex.ru/auth/session/', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/x-www-form-urlencoded',
					'Ya-Consumer-Authorization': 'OAuth %s'
				},
				body: 'type=oauth&oauth_token=%s&retpath=https%%3A%%2F%%2Fbusiness.yandex.ru',
				credentials: 'include',
				redirect: 'manual'
			});
			return JSON.stringify({ok: true, status: resp.status});
		} catch(e) {
			return JSON.stringify({ok: false, error: e.message});
		}
	}`, oauthToken, oauthToken)

	_, _ = page.Evaluate(script)

	cookies, err := bCtx.Cookies(yandexPassportCookieHost, yandexCookieHost)
	if err != nil {
		return fmt.Errorf("read cookies after exchange: %w", err)
	}
	for _, c := range cookies {
		if c.Name == "Session_id" || c.Name == "sessionid2" {
			_, err = page.Goto(yandexBusinessBaseURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(pageHydrateTimeoutMs),
			})
			return err
		}
	}

	return fmt.Errorf("oauth session exchange failed: no Session_id cookie received (token may lack required scope)")
}

// isOAuthToken returns true if the value looks like an OAuth token rather than cookies JSON.
func isOAuthToken(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && !strings.HasPrefix(trimmed, "[")
}
