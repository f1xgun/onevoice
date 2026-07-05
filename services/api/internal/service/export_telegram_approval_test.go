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

// HandleForTest exposes the unexported /start owner-link bind core so the
// external service_test package can drive it without a live NATS subscription.
// Test-only.
func (c *TelegramOwnerLinkConsumer) HandleForTest(ctx context.Context, link a2a.TelegramOwnerLink) error {
	return c.handle(ctx, link)
}
