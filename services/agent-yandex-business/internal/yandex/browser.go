package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/playwright-community/playwright-go"
)

const businessURL = "https://business.yandex.ru"

// Browser implements YandexBrowser using Playwright for RPA automation.
type Browser struct {
	cookiesJSON string
}

// NewBrowser creates a Browser with the given Yandex session cookies (JSON array).
func NewBrowser(cookiesJSON string) *Browser {
	return &Browser{cookiesJSON: cookiesJSON}
}

// withPage creates a headless Chromium page, injects cookies, and calls fn.
// A screenshot is saved to /tmp/yandex_error_*.png on error for diagnostics.
func (b *Browser) withPage(ctx context.Context, fn func(page playwright.Page) error) error {
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright: run: %w", err)
	}
	defer pw.Stop() //nolint:errcheck

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
			"--no-sandbox",
		},
	})
	if err != nil {
		return fmt.Errorf("playwright: launch: %w", err)
	}
	defer browser.Close()

	bCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	})
	if err != nil {
		return fmt.Errorf("playwright: new context: %w", err)
	}
	defer bCtx.Close()

	if err := b.setCookies(bCtx); err != nil {
		return fmt.Errorf("playwright: set cookies: %w", err)
	}

	page, err := bCtx.NewPage()
	if err != nil {
		return fmt.Errorf("playwright: new page: %w", err)
	}

	if err := fn(page); err != nil {
		filename := fmt.Sprintf("/tmp/yandex_error_%d.png", time.Now().UnixMilli())
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filename)})
		return err
	}
	return nil
}

func (b *Browser) setCookies(bCtx playwright.BrowserContext) error {
	var cookies []map[string]interface{}
	if err := json.Unmarshal([]byte(b.cookiesJSON), &cookies); err != nil {
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

// humanDelay waits 1–4 seconds to mimic human browsing behavior.
func humanDelay() {
	time.Sleep(time.Duration(rand.Intn(3000)+1000) * time.Millisecond)
}

// withRetry retries fn up to maxAttempts times with exponential backoff (2^i seconds).
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for i := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
