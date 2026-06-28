package yandex

import (
	"context"
	"fmt"
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

	inputSelector := ".MediaUploadButton-Input"
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

	return bb.runStep(ctx, "uploadPhoto", 3, func(page playwright.Page) error {
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

		body, err := bb.downloadPhoto(ctx, photoURL)
		if err != nil {
			return err
		}

		tmpFile, err := stageTempPhoto(body)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmpFile) }()

		fileInput := page.Locator(inputSelector).First()
		if err := fileInput.SetInputFiles(tmpFile); err != nil {
			debugScreenshot(page, "photo_input_error")
			return fmt.Errorf("set file input (%s): %w", inputSelector, err)
		}

		time.Sleep(3 * time.Second)
		debugScreenshot(page, "photo_after_upload")

		cropSaveBtn := page.Locator("button:has-text('Сохранить')").First()
		if err := cropSaveBtn.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(primaryActionTimeoutMs),
			State:   playwright.WaitForSelectorStateVisible,
		}); err == nil {
			if err := cropSaveBtn.Click(); err != nil {
				debugScreenshot(page, "photo_crop_save_error")
				return fmt.Errorf("click crop save button: %w", err)
			}
			time.Sleep(3 * time.Second)
			debugScreenshot(page, "photo_after_crop_save")
		}
		return nil
	})
}

// stageTempPhoto writes a downloaded image to a unique temp file and returns its
// path. It uses os.CreateTemp so concurrent uploads (the BrowserPool runs
// multiple business contexts in parallel) never share a path: a predictable
// name would let one upload clobber another tenant's bytes or have a sibling's
// defer-Remove delete the file before SetInputFiles reads it.
func stageTempPhoto(body []byte) (string, error) {
	f, err := os.CreateTemp("", "upload-*.jpg")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	name := f.Name()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(name, tmpFileMode); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	return name, nil
}
