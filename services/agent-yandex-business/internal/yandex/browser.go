package yandex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/playwright-community/playwright-go"
)

// debugScreenshots is true when RPA_DEBUG_SCREENSHOTS env is set.
var debugScreenshots = os.Getenv("RPA_DEBUG_SCREENSHOTS") != ""

// debugScreenshot saves a screenshot to /tmp/rpa_debug_{label}_{timestamp}.png
// when RPA_DEBUG_SCREENSHOTS is enabled.
func debugScreenshot(page playwright.Page, label string) {
	if !debugScreenshots {
		return
	}
	filename := fmt.Sprintf("/tmp/rpa_debug_%s_%d.png", label, time.Now().UnixMilli())
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(filename),
		FullPage: playwright.Bool(true),
	}); err != nil {
		slog.Warn("debug screenshot failed", "label", label, "error", err)
		return
	}
	slog.Info("debug screenshot saved", "label", label, "path", filename)
}

// spravBaseURL builds the Yandex.Business management URL for a given permalink.
func spravBaseURL(permalink string) string {
	if permalink == "" || permalink == "default" {
		return "https://business.yandex.ru"
	}
	return "https://yandex.ru/sprav/" + permalink + "/p/edit"
}

// humanDelay waits 1-4 seconds to mimic human browsing behavior.
func humanDelay() {
	time.Sleep(time.Duration(rand.Intn(3000)+1000) * time.Millisecond) //nolint:gosec // weak random is intentional for human-like delay simulation
}

// withRetry retries fn up to maxAttempts times with exponential backoff (2^i seconds).
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error { //nolint:unparam // keeping maxAttempts as parameter for flexibility
	var lastErr error
	for i := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if errors.Is(lastErr, &a2a.NonRetryableError{}) {
			return lastErr
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(1<<i) * time.Second)
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
