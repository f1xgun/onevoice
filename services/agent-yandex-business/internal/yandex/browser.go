package yandex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// ScreenshotMode controls how diagnostic page screenshots are captured by the
// Yandex.Business RPA agent. The value is read from the SCREENSHOT_MODE env on
// every call to screenshotMode so an operator can flip to "off" mid-incident
// without restarting the agent.
type ScreenshotMode string

const (
	// ScreenshotOff captures nothing — no file is written and page.Screenshot
	// is not invoked. Production default; protects PII in the authenticated
	// Yandex.Business DOM.
	ScreenshotOff ScreenshotMode = "off"

	// ScreenshotTmpfs writes screenshots under /dev/shm with a 1h TTL sweeper.
	// Staging default — data is ephemeral, never persisted to disk.
	ScreenshotTmpfs ScreenshotMode = "tmpfs"

	// ScreenshotFull writes screenshots under /tmp with no TTL. Dev default —
	// files outlive the agent for post-mortem inspection.
	ScreenshotFull ScreenshotMode = "full"
)

// screenshotTmpfsDir is where tmpfs-mode screenshots live and where the sweeper
// reaps files older than the screenshot TTL.
const screenshotTmpfsDir = "/dev/shm/onevoice-rpa-screenshots"

// screenshotTTL is how long a tmpfs-mode screenshot survives before the sweeper
// removes it.
const screenshotTTL = 1 * time.Hour

// screenshotSweepInterval is the polling cadence of the sweeper goroutine.
const screenshotSweepInterval = 5 * time.Minute

// screenshotDirPerm is the mode applied to the tmpfs screenshot directory.
// 0o750 satisfies gosec G301: owner full, group read+exec, world none.
const screenshotDirPerm = 0o750

// screenshotMode reads SCREENSHOT_MODE on each call. Unknown values fall back
// to off — the safest default whenever the env contract is violated.
func screenshotMode() ScreenshotMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SCREENSHOT_MODE"))) {
	case "tmpfs":
		return ScreenshotTmpfs
	case "full":
		return ScreenshotFull
	default:
		return ScreenshotOff
	}
}

// captureScreenshot writes a screenshot of the current page under a label.
// Returns ("", nil) when SCREENSHOT_MODE=off and does not invoke page.Screenshot.
// In tmpfs/full mode it returns the absolute path the file was written to.
func captureScreenshot(page playwright.Page, label string) (string, error) {
	mode := screenshotMode()
	if mode == ScreenshotOff {
		return "", nil
	}
	ts := time.Now().UnixMilli()
	var path string
	switch mode {
	case ScreenshotTmpfs:
		if err := os.MkdirAll(screenshotTmpfsDir, screenshotDirPerm); err != nil {
			return "", fmt.Errorf("mkdir tmpfs screenshot dir: %w", err)
		}
		path = filepath.Join(screenshotTmpfsDir, fmt.Sprintf("yandex_%s_%d.png", label, ts))
	case ScreenshotFull:
		path = fmt.Sprintf("/tmp/yandex_%s_%d.png", label, ts)
	default:
		return "", nil
	}
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(path),
		FullPage: playwright.Bool(true),
	}); err != nil {
		return path, err
	}
	return path, nil
}

// debugScreenshot is the thin wrapper used by the existing RPA tool sites.
// It applies the SCREENSHOT_MODE gate and logs success/failure. Files captured
// in tmpfs mode are reclaimed by StartScreenshotSweeper; files captured in
// full mode survive for post-mortem inspection in development.
func debugScreenshot(page playwright.Page, label string) {
	path, err := captureScreenshot(page, label)
	if err != nil {
		slog.Warn("screenshot capture failed", "label", label, "error", err)
		return
	}
	if path == "" {
		return
	}
	slog.Info("debug screenshot saved", "label", label, "path", path)
}

// StartScreenshotSweeper spawns a goroutine that removes screenshots older
// than screenshotTTL from the tmpfs directory every screenshotSweepInterval.
// The goroutine exits when ctx is canceled (bind via signal.NotifyContext for
// graceful SIGTERM shutdown). It is a no-op when SCREENSHOT_MODE is not tmpfs.
func StartScreenshotSweeper(ctx context.Context, logger *slog.Logger) {
	if screenshotMode() != ScreenshotTmpfs {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	startSweeperLoop(ctx, logger, screenshotSweepInterval, screenshotTmpfsDir, nil)
}

// startSweeperForTest is the test-only seam used by browser_test.go. It runs
// the sweeper loop against a caller-controlled tick interval and signals
// `done` after the loop exits so the test can assert clean shutdown without
// waiting for the production five-minute cadence.
func startSweeperForTest(ctx context.Context, logger *slog.Logger, interval time.Duration, done chan<- struct{}) {
	startSweeperLoop(ctx, logger, interval, screenshotTmpfsDir, done)
}

func startSweeperLoop(ctx context.Context, logger *slog.Logger, interval time.Duration, dir string, done chan<- struct{}) {
	go func() {
		if done != nil {
			defer close(done)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweepScreenshotsIn(dir, time.Now().Add(-screenshotTTL), logger)
			}
		}
	}()
}

// sweepScreenshotsIn removes files in dir whose modtime is before cutoff. It
// is the test-friendly seam exercised by TestSweepScreenshotsIn_RemovesOldFiles.
func sweepScreenshotsIn(dir string, cutoff time.Time, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				logger.Debug("screenshot sweep remove failed", "name", e.Name(), "error", err)
			}
		}
	}
}

// spravBaseURL builds the Yandex.Business management URL for a given permalink.
func spravBaseURL(permalink string) string {
	if permalink == "" || permalink == "default" {
		return yandexBusinessBaseURL
	}
	return fmt.Sprintf(yandexSpravEditPathFmt, permalink)
}

// humanDelay waits 1-4 seconds to mimic human browsing behavior.
func humanDelay() {
	time.Sleep(time.Duration(rand.Intn(humanDelayRangeMs)+humanDelayMinMs) * time.Millisecond) //nolint:gosec // weak random is intentional for human-like delay simulation
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
		if errors.Is(lastErr, &a2a.NonRetryableError{}) {
			return lastErr
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(1<<i) * time.Second)
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
