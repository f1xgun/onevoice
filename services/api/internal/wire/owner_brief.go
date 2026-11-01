package wire

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// StartOwnerBrief remains for compatibility. Proactive owner Telegram messages
// are disabled globally, so even legacy enabled settings cannot start a worker.
func (s *Services) StartOwnerBrief(ctx context.Context, wg *sync.WaitGroup, log *slog.Logger, enabled bool, pollInterval time.Duration) {
}
