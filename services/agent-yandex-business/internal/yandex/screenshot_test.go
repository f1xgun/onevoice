package yandex

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestScreenshotMode_Off(t *testing.T) {
	t.Setenv("SCREENSHOT_MODE", "")

	if got := screenshotMode(); got != ScreenshotOff {
		t.Fatalf("default screenshotMode = %q, want %q", got, ScreenshotOff)
	}

	page := newMockPage("https://yandex.example/")
	path, err := captureScreenshot(page, "x")
	if err != nil {
		t.Fatalf("captureScreenshot returned error in off mode: %v", err)
	}
	if path != "" {
		t.Fatalf("off mode must return empty path, got %q", path)
	}
	if len(page.screenshotPaths) != 0 {
		t.Fatalf("off mode must NOT call page.Screenshot, got paths=%v", page.screenshotPaths)
	}
}

func TestScreenshotMode_InvalidValue(t *testing.T) {
	t.Setenv("SCREENSHOT_MODE", "garbage")

	if got := screenshotMode(); got != ScreenshotOff {
		t.Fatalf("unknown value must default to off, got %q", got)
	}
}

func TestScreenshotMode_Tmpfs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("tmpfs mode test requires /dev/shm, skipping on %s", runtime.GOOS)
	}
	if err := os.MkdirAll(screenshotTmpfsDir, screenshotDirPerm); err != nil {
		t.Skipf("/dev/shm not writable in this environment: %v", err)
	}

	t.Setenv("SCREENSHOT_MODE", "tmpfs")

	if got := screenshotMode(); got != ScreenshotTmpfs {
		t.Fatalf("screenshotMode = %q, want tmpfs", got)
	}

	page := newMockPage("https://yandex.example/")
	path, err := captureScreenshot(page, "x")
	if err != nil {
		t.Fatalf("captureScreenshot in tmpfs mode: %v", err)
	}
	pattern := `^/dev/shm/onevoice-rpa-screenshots/yandex_x_\d+\.png$`
	if matched, _ := regexp.MatchString(pattern, path); !matched {
		t.Fatalf("tmpfs path %q does not match %s", path, pattern)
	}
	if len(page.screenshotPaths) != 1 || page.screenshotPaths[0] != path {
		t.Fatalf("page.Screenshot Path = %v, want [%q]", page.screenshotPaths, path)
	}
	info, err := os.Stat(screenshotTmpfsDir)
	if err != nil {
		t.Fatalf("tmpfs dir missing after capture: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("tmpfs dir path is not a directory")
	}
}

func TestScreenshotMode_Full(t *testing.T) {
	t.Setenv("SCREENSHOT_MODE", "full")

	if got := screenshotMode(); got != ScreenshotFull {
		t.Fatalf("screenshotMode = %q, want full", got)
	}

	page := newMockPage("https://yandex.example/")
	path, err := captureScreenshot(page, "x")
	if err != nil {
		t.Fatalf("captureScreenshot in full mode: %v", err)
	}
	pattern := `^/tmp/yandex_x_\d+\.png$`
	if matched, _ := regexp.MatchString(pattern, path); !matched {
		t.Fatalf("full path %q does not match %s", path, pattern)
	}
	if len(page.screenshotPaths) != 1 || page.screenshotPaths[0] != path {
		t.Fatalf("page.Screenshot Path = %v, want [%q]", page.screenshotPaths, path)
	}
}

func TestScreenshotMode_CaseInsensitive(t *testing.T) {
	t.Setenv("SCREENSHOT_MODE", "  Full  ")
	if got := screenshotMode(); got != ScreenshotFull {
		t.Fatalf("mixed-case + whitespace value must normalise, got %q", got)
	}
}

func TestSweepScreenshotsIn_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	cutoff := time.Now().Add(-1 * time.Hour)

	old := filepath.Join(dir, "yandex_old.png")
	fresh := filepath.Join(dir, "yandex_fresh.png")
	if err := os.WriteFile(old, []byte("o"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("f"), 0o600); err != nil {
		t.Fatal(err)
	}
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}

	sweepScreenshotsIn(dir, cutoff, slog.Default())

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh file must be retained, stat err = %v", err)
	}
}

func TestSweepScreenshotsIn_MissingDirIsNoop(t *testing.T) {
	sweepScreenshotsIn(filepath.Join(t.TempDir(), "missing"), time.Now(), slog.Default())
}

func TestStartScreenshotSweeper_ExitsCleanlyWhenOff(t *testing.T) {
	t.Setenv("SCREENSHOT_MODE", "off")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	startSweeperForTest(ctx, slog.Default(), 10*time.Millisecond, done)

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sweeper goroutine did not exit within 500ms of ctx cancel in off mode")
	}
}

func TestStartScreenshotSweeper_RuntimeFlipOffToTmpfs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("runtime-flip test requires /dev/shm, skipping on %s", runtime.GOOS)
	}
	if err := os.MkdirAll(screenshotTmpfsDir, screenshotDirPerm); err != nil {
		t.Skipf("/dev/shm not writable in this environment: %v", err)
	}
	t.Setenv("SCREENSHOT_MODE", "off")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	startSweeperForTest(ctx, slog.Default(), 10*time.Millisecond, done)

	old := filepath.Join(screenshotTmpfsDir, "yandex_runtime_flip.png")
	if err := os.WriteFile(old, []byte("o"), 0o600); err != nil {
		t.Fatal(err)
	}
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expected backdated file to be swept even in off mode, stat err = %v", err)
	}
}

func TestStartScreenshotSweeper_ExitsOnContextCancel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("sweeper exit test requires tmpfs mode, skipping on %s", runtime.GOOS)
	}
	if err := os.MkdirAll(screenshotTmpfsDir, screenshotDirPerm); err != nil {
		t.Skipf("/dev/shm not writable in this environment: %v", err)
	}
	t.Setenv("SCREENSHOT_MODE", "tmpfs")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	startSweeperForTest(ctx, slog.Default(), 10*time.Millisecond, done)

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("sweeper goroutine did not exit within 500ms of ctx cancel")
	}
}

func TestScreenshotMode_Constants(t *testing.T) {
	if string(ScreenshotOff) != "off" ||
		string(ScreenshotTmpfs) != "tmpfs" ||
		string(ScreenshotFull) != "full" {
		t.Fatalf("ScreenshotMode constants drifted: off=%q tmpfs=%q full=%q",
			ScreenshotOff, ScreenshotTmpfs, ScreenshotFull)
	}
	t.Setenv("SCREENSHOT_MODE", "full")
	if !strings.EqualFold(string(screenshotMode()), "full") {
		t.Fatalf("screenshotMode does not re-read env on each call")
	}
}
