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

	msg := &natslib.Msg{Subject: "integrations.revoked.telegram.11111111-2222-3333-4444-555555555555"}
	handleRevokeMessage(msg, "telegram", inv, rec)

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
}

func TestHandleRevokeMessage_MalformedSubjectSkipped(t *testing.T) {
	var invoked, recorded bool
	inv := func(_, _, _ string) { invoked = true }
	rec := func(_ string) { recorded = true }

	for _, subj := range []string{
		"integrations.revoked.telegram",
		"integrations.revoked",
		"integrations.revoked.telegram.biz.extra",
		"",
	} {
		handleRevokeMessage(&natslib.Msg{Subject: subj}, "telegram", inv, rec)
	}

	if invoked {
		t.Fatalf("expected no Invalidate call on malformed subjects")
	}
	if recorded {
		t.Fatalf("expected no metric record on malformed subjects")
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
