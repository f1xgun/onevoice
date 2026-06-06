package agentbase

import (
	"sync"
	"testing"

	natslib "github.com/nats-io/nats.go"
)

type invalidateCall struct {
	businessID string
	platform   string
	externalID string
}

func TestHandleRevokeMessage_ValidSubject(t *testing.T) {
	var (
		mu       sync.Mutex
		invCalls []invalidateCall
		metrics  []string
		dropped  []string
	)
	inv := func(biz, platform, ext string) {
		mu.Lock()
		defer mu.Unlock()
		invCalls = append(invCalls, invalidateCall{biz, platform, ext})
	}
	rec := func(platform string) {
		mu.Lock()
		defer mu.Unlock()
		metrics = append(metrics, platform)
	}
	drop := func(platform string) {
		mu.Lock()
		defer mu.Unlock()
		dropped = append(dropped, platform)
	}

	msg := &natslib.Msg{Subject: "integrations.revoked.telegram.11111111-2222-3333-4444-555555555555"}
	handleRevokeMessage(msg, "telegram", inv, rec, drop)

	if len(invCalls) != 1 {
		t.Fatalf("expected 1 invalidate call, got %d", len(invCalls))
	}
	got := invCalls[0]
	if got.businessID != "11111111-2222-3333-4444-555555555555" || got.platform != "telegram" || got.externalID != "" {
		t.Fatalf("unexpected invalidate args: %+v", got)
	}
	if len(metrics) != 1 || metrics[0] != "telegram" {
		t.Fatalf("expected one metric record for telegram, got %v", metrics)
	}
	if len(dropped) != 0 {
		t.Fatalf("expected no dropped record on a valid subject, got %v", dropped)
	}
}

func TestHandleRevokeMessage_MalformedSubjectSkipped(t *testing.T) {
	var invoked, recorded bool
	var dropped []string
	inv := func(_, _, _ string) { invoked = true }
	rec := func(_ string) { recorded = true }
	drop := func(platform string) { dropped = append(dropped, platform) }

	subjects := []string{
		"integrations.revoked.telegram",
		"integrations.revoked",
		"integrations.revoked.telegram.biz.extra",
		"",
	}
	for _, subj := range subjects {
		handleRevokeMessage(&natslib.Msg{Subject: subj}, "telegram", inv, rec, drop)
	}

	if invoked {
		t.Fatalf("expected no Invalidate call on malformed subjects")
	}
	if recorded {
		t.Fatalf("expected no received-metric record on malformed subjects")
	}
	if len(dropped) != len(subjects) {
		t.Fatalf("expected %d dropped records, got %d (%v)", len(subjects), len(dropped), dropped)
	}
	for _, p := range dropped {
		if p != "telegram" {
			t.Fatalf("expected dropped record labeled telegram, got %q", p)
		}
	}
}

func TestNewRevokeSubscriber_Subject(t *testing.T) {
	if got := revokeSubject("yandex_business"); got != "integrations.revoked.yandex_business.*" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestRevokeSubscriber_CloseNilSafe(t *testing.T) {
	var r *RevokeSubscriber
	if err := r.Close(); err != nil {
		t.Fatalf("nil receiver Close should be nil, got %v", err)
	}
	r = &RevokeSubscriber{}
	if err := r.Close(); err != nil {
		t.Fatalf("nil-subscription Close should be nil, got %v", err)
	}
}
