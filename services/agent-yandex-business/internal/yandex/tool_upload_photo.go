package yandex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// UploadPhoto uploads a photo to Yandex.Business from a public URL.
// category: "general" (main photos), "logo", "services", "enter", "interior", "goods", "exterior"
func (bb *BusinessBrowser) UploadPhoto(ctx context.Context, photoURL, category string) error {
	if photoURL == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("photo_url is required"))
	}

	// Map category to file input selector
	inputSelector := ".MediaUploadButton-Input" // default: general photo upload button
	switch category {
	case "logo":
		inputSelector = ".MediaAttach-Input[name='logo']"
	case "services":
		inputSelector = ".MediaAttach-Input[name='services']"
	case "enter", "entrance":
		inputSelector = ".MediaAttach-Input[name='enter']"
	case "interior":
		inputSelector = ".MediaAttach-Input[name='interior']"
	case "goods":
		inputSelector = ".MediaAttach-Input[name='goods']"
	case "exterior":
		inputSelector = ".MediaAttach-Input[name='exterior']"
	}

	return recordStep("uploadPhoto", func() error {
		return withRetry(ctx, 3, func() error {
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
				photosURL := bb.baseURL() + "/photos/"
				if _, err := page.Goto(photosURL, playwright.PageGotoOptions{
					WaitUntil: playwright.WaitUntilStateNetworkidle,
					Timeout:   playwright.Float(pageNavTimeoutMs),
				}); err != nil {
					debugScreenshot(page, "photo_navigate_error")
					return fmt.Errorf("navigate to photos page: %w", err)
				}
				closePopups(page)
				if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
					return err
				}
				humanDelay()

				// Download the image to a temp file using standard HTTP
				httpResp, err := http.Get(photoURL) //nolint:gosec // URL comes from LLM/user, external fetch is intentional
				if err != nil {
					return fmt.Errorf("download photo from %s: %w", photoURL, err)
				}
				body, err := io.ReadAll(httpResp.Body)
				_ = httpResp.Body.Close()
				if err != nil {
					return fmt.Errorf("read photo body: %w", err)
				}
				if len(body) == 0 {
					return fmt.Errorf("downloaded empty file from %s", photoURL)
				}

				tmpFile := fmt.Sprintf("/tmp/upload_%d.jpg", time.Now().UnixMilli())
				if err := os.WriteFile(tmpFile, body, tmpFileMode); err != nil {
					return fmt.Errorf("write temp file: %w", err)
				}
				defer func() { _ = os.Remove(tmpFile) }()

				// Set file on the hidden input
				fileInput := page.Locator(inputSelector).First()
				if err := fileInput.SetInputFiles(tmpFile); err != nil {
					debugScreenshot(page, "photo_input_error")
					return fmt.Errorf("set file input (%s): %w", inputSelector, err)
				}

				// Wait for upload processing
				time.Sleep(3 * time.Second)
				debugScreenshot(page, "photo_after_upload")

				// Handle crop dialog if it appears (logo uploads show a crop modal)
				cropSaveBtn := page.Locator("button:has-text('Сохранить')").First()
				if err := cropSaveBtn.WaitFor(playwright.LocatorWaitForOptions{
					Timeout: playwright.Float(primaryActionTimeoutMs),
					State:   playwright.WaitForSelectorStateVisible,
				}); err == nil {
					_ = cropSaveBtn.Click()
					time.Sleep(3 * time.Second)
					debugScreenshot(page, "photo_after_crop_save")
				}
				return nil
			})
		})
	})
}
