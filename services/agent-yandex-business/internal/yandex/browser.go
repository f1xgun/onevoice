package yandex

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

const businessURL = "https://business.yandex.ru"

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
			time.Sleep(time.Duration(1<<uint(i)) * time.Second) //nolint:gosec // i is bounded by maxAttempts (small value), no overflow risk
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}
