package yandex

import (
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// recordStep wraps an RPA helper invocation and emits a single
// rpa_step_duration_seconds observation labeled by the caller-supplied step
// name (hard-coded — never derived from runtime variables) and the binary
// result ("ok" on nil error, "error" otherwise).
func recordStep(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.RPAStepDuration.WithLabelValues(name, result).Observe(time.Since(start).Seconds())
	return err
}

// closePopups dismisses common Yandex popups that overlay the page.
// Used by ListCompanies, GetReviews, navigateToEditPage (which feeds GetInfo /
// UpdateInfo / UpdateHours), CreatePost, and UploadPhoto — i.e. essentially
// every tool that lands on a page Yandex may decorate with a modal.
func closePopups(page playwright.Page) {
	closeBtnSelectors := []string{
		".InfoModal-IconClose",
		".CrossPlatformModal-Close",
		"button[aria-label='Закрыть']",
		".Modal-Close",
	}
	for _, sel := range closeBtnSelectors {
		btn := page.Locator(sel).First()
		if err := btn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(clickAwayTimeoutMs)}); err == nil {
			time.Sleep(dialogSettleDelay)
		}
	}
}

// clickSave clicks the "Сохранить изменения" button.
// Used by UpdateInfo and UpdateHours after they fill the relevant inputs.
func clickSave(page playwright.Page) error {
	saveBtn := page.Locator(".SaveButton-Button").First()
	if err := saveBtn.WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(primaryActionTimeoutMs),
		State:   playwright.WaitForSelectorStateVisible,
	}); err != nil {
		debugScreenshot(page, "save_not_found")
		return fmt.Errorf("save button not found")
	}
	if err := saveBtn.Click(); err != nil {
		return fmt.Errorf("click save: %w", err)
	}
	humanDelay()
	return nil
}
