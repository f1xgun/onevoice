// Package payments holds the Track-B payment-provider seam. It is intentionally
// declaration-only in Track-A: no implementation, no wiring. It exists now so
// the checkout / webhook / receipt write paths can be added later WITHOUT
// reshaping the billing schema (subscriptions already carries the nullable
// provider / provider_sub_id columns).
//
// The concrete provider in the roadmap is ЮKassa (YooKassa); Track-B adds the
// implementation and the routes (POST checkout, webhook receiver), a
// payment_events table, fiscal receipts, and the 402 / lapse-sweeper flows.
package payments

import "context"

// CheckoutRequest describes a subscription checkout to open.
type CheckoutRequest struct {
	BusinessID  string
	PlanCode    string
	ReturnURL   string
	Description string
}

// CheckoutResult is the provider's response to a checkout creation.
type CheckoutResult struct {
	CheckoutID  string
	RedirectURL string
}

// WebhookEvent is the normalized shape of a provider webhook after signature
// verification.
type WebhookEvent struct {
	Type          string
	ProviderSubID string
	Status        string
}

// ReceiptRequest describes a fiscal receipt to issue for a payment.
type ReceiptRequest struct {
	PaymentID string
	AmountRUB float64
	Email     string
}

// ReceiptResult is the provider's response to a receipt issuance.
type ReceiptResult struct {
	ReceiptID string
}

// PaymentProvider is the Track-B seam. No production implementation exists in
// Track-A; every method is unimplemented until Track-B lands.
type PaymentProvider interface {
	// CreateCheckout opens a hosted-payment session for a plan purchase.
	CreateCheckout(ctx context.Context, req CheckoutRequest) (CheckoutResult, error)
	// HandleWebhook verifies and normalizes a provider callback.
	HandleWebhook(ctx context.Context, payload []byte, signature string) (WebhookEvent, error)
	// IssueReceipt requests a fiscal receipt for a settled payment.
	IssueReceipt(ctx context.Context, req ReceiptRequest) (ReceiptResult, error)
}
