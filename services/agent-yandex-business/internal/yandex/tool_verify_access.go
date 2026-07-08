package yandex

import (
	"context"

	"github.com/playwright-community/playwright-go"
)

// verifyAccessFormSelector is the edit-page root that only mounts when the
// representative account actually has access to the org. If the account lacks
// access Yandex redirects to passport (login) or serves a 403 shell without
// this form, so its presence is the access signal.
const verifyAccessFormSelector = ".SidebarCompanyInfo, [class*='CompanyName'], .WorkIntervalsUnificationInput-Input"

// VerifyAccess confirms the SHARED representative session can reach and edit the
// org identified by bb.permalink. It navigates directly to
// /sprav/<permalink>/p/edit and reports whether the edit form mounted without a
// passport/403 redirect. It is the connect-time probe behind the delegated
// verify-access endpoint.
//
// Isolation: bb.permalink comes from the business's own integration row, and
// after navigation the canary + assertPermalinkSegment confirm the live URL is
// this org's /sprav/<permalink>/ — so a mismatched permalink can never report a
// false positive against another tenant's org.
//
// The bool return is the access verdict: true = form mounted (access
// confirmed), false = no form (access not detected, e.g. representative not yet
// added). A session-expired / captcha canary error is returned as an error (not
// a false verdict) so the shared session is evicted and the operator is alerted
// rather than misreading a dead session as "no access".
func (bb *BusinessBrowser) VerifyAccess(ctx context.Context) (bool, error) {
	var detected bool
	err := bb.runStep(ctx, "verifyAccess", 2, func(page playwright.Page) error {
		editURL := bb.baseURL() + "/"
		if _, err := page.Goto(editURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
			Timeout:   playwright.Float(pageNavTimeoutMs),
		}); err != nil {
			return err
		}
		closePopups(page)

		if err := bb.guardSession(page); err != nil {
			return err
		}

		form := page.Locator(verifyAccessFormSelector).First()
		if err := form.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(pageHydrateTimeoutMs),
			State:   playwright.WaitForSelectorStateVisible,
		}); err != nil {
			detected = false
			return nil
		}
		detected = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return detected, nil
}
