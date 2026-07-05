package service

import (
	"context"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// HandleForTest exposes the unexported validated resolve+resume+audit core to
// the external service_test package so the SAFETY-GATE table can drive it
// directly without a live NATS subscription. Test-only (compiled only under
// _test.go).
func (c *TelegramApprovalConsumer) HandleForTest(ctx context.Context, cb a2a.TelegramApprovalCallback) error {
	return c.handle(ctx, cb)
}
