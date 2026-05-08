package service

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestBuildPlatformReply_VK(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentVK, ExternalID: "11_42"}
	tool, args, err := buildPlatformReply(r, "Спасибо!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "vk__reply_comment" {
		t.Errorf("tool = %q", tool)
	}
	if args["post_id"].(float64) != 11 || args["comment_id"].(float64) != 42 || args["text"].(string) != "Спасибо!" {
		t.Errorf("args = %+v", args)
	}
}

func TestBuildPlatformReply_VKMalformedExternalID(t *testing.T) {
	cases := []string{"", "11", "abc_42", "11_xyz"}
	for _, ext := range cases {
		t.Run(ext, func(t *testing.T) {
			r := &domain.Review{Platform: a2a.AgentVK, ExternalID: ext}
			if _, _, err := buildPlatformReply(r, "x"); err == nil {
				t.Errorf("expected error for external_id=%q", ext)
			}
		})
	}
}

func TestBuildPlatformReply_Telegram(t *testing.T) {
	r := &domain.Review{
		Platform:     a2a.AgentTelegram,
		ID:           "r1",
		ExternalID:   "-1003615540583_21",
		PlatformMeta: map[string]interface{}{"chat_id": float64(-1003615540583), "message_id": float64(21)},
	}
	tool, args, err := buildPlatformReply(r, "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "telegram__reply_to_comment" {
		t.Errorf("tool = %q", tool)
	}
	if args["chat_id"].(string) != "-1003615540583" {
		t.Errorf("chat_id = %v", args["chat_id"])
	}
	if args["message_id"].(float64) != 21 {
		t.Errorf("message_id = %v", args["message_id"])
	}
	if args["text"].(string) != "hi" {
		t.Errorf("text = %v", args["text"])
	}
}

func TestBuildPlatformReply_TelegramMissingMeta(t *testing.T) {
	cases := []map[string]interface{}{
		nil,
		{},
		{"chat_id": "x"},                 // missing message_id
		{"message_id": float64(21)},      // missing chat_id
		{"chat_id": "", "message_id": 1}, // empty chat_id
	}
	for i, meta := range cases {
		r := &domain.Review{Platform: a2a.AgentTelegram, ID: "r", PlatformMeta: meta}
		if _, _, err := buildPlatformReply(r, "x"); err == nil {
			t.Errorf("case %d expected error for meta=%+v", i, meta)
		}
	}
}

func TestBuildPlatformReply_Yandex(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentYandexBusiness, ExternalID: "yreview-77"}
	tool, args, err := buildPlatformReply(r, "thx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "yandex_business__reply_review" {
		t.Errorf("tool = %q", tool)
	}
	if args["review_id"].(string) != "yreview-77" || args["text"].(string) != "thx" {
		t.Errorf("args = %+v", args)
	}
}

func TestBuildPlatformReply_YandexEmptyExternal(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentYandexBusiness, ExternalID: ""}
	if _, _, err := buildPlatformReply(r, "x"); err == nil {
		t.Errorf("expected error for empty external_id")
	}
}

func TestBuildPlatformReply_Google(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentGoogleBusiness, ExternalID: "accounts/1/locations/2/reviews/3"}
	tool, args, err := buildPlatformReply(r, "thx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "google_business__reply_review" {
		t.Errorf("tool = %q", tool)
	}
	if args["review_name"].(string) != "accounts/1/locations/2/reviews/3" {
		t.Errorf("review_name not forwarded: %+v", args)
	}
}

func TestBuildPlatformReply_UnknownPlatformIsNoop(t *testing.T) {
	r := &domain.Review{Platform: "future_platform", ExternalID: "abc"}
	tool, args, err := buildPlatformReply(r, "x")
	if err != nil {
		t.Errorf("unknown platform should not error, got %v", err)
	}
	if tool != "" || args != nil {
		t.Errorf("unknown platform should produce no dispatch: tool=%q args=%+v", tool, args)
	}
}

func TestMetaInt_AcceptsFloatAndInt(t *testing.T) {
	cases := []map[string]interface{}{
		{"k": float64(42)},
		{"k": int(42)},
		{"k": int64(42)},
		{"k": "42"},
	}
	for i, m := range cases {
		got, ok := metaInt(m, "k")
		if !ok || got != 42 {
			t.Errorf("case %d: got=%d ok=%v", i, got, ok)
		}
	}
}

func TestMetaString_NormalizesNumeric(t *testing.T) {
	if got, ok := metaString(map[string]interface{}{"k": float64(-1003615540583)}, "k"); !ok || got != "-1003615540583" {
		t.Errorf("float64 normalize: got=%q ok=%v", got, ok)
	}
	if got, ok := metaString(map[string]interface{}{"k": "abc"}, "k"); !ok || got != "abc" {
		t.Errorf("string passthrough: got=%q ok=%v", got, ok)
	}
	if _, ok := metaString(nil, "k"); ok {
		t.Error("nil map should return false")
	}
	if _, ok := metaString(map[string]interface{}{"k": ""}, "k"); ok {
		t.Error("empty string should return false")
	}
}
