package service

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestBuildPlatformReply_VK(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentVK, ExternalID: "11_42"}
	tool, args, err := buildPlatformReply(r, "Спасибо!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.VKReplyComment {
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
	if tool != tools.TelegramReplyToComment {
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
		{"chat_id": "x"},
		{"message_id": float64(21)},
		{"chat_id": "", "message_id": 1},
	}
	for i, meta := range cases {
		r := &domain.Review{Platform: a2a.AgentTelegram, ID: "r", PlatformMeta: meta}
		if _, _, err := buildPlatformReply(r, "x"); err == nil {
			t.Errorf("case %d expected error for meta=%+v", i, meta)
		}
	}
}

// Ingestion → reply regression for P0-4: a Telegram review map (the shape the
// agent returns, numbers as JSON float64) must carry chat_id+message_id into
// PlatformMeta so the reply that follows can target the original message.
func TestReviewFromMap_TelegramPopulatesPlatformMeta(t *testing.T) {
	m := map[string]interface{}{
		"id":         "-1003615540583_21",
		"message_id": float64(21),
		"chat_id":    float64(-1003615540583),
		"author":     "Иван",
		"text":       "Отличный сервис",
		"created_at": "2026-06-12T10:00:00Z",
	}
	review := reviewFromMap(m, "biz-1", a2a.AgentTelegram)

	chatID, ok := metaInt(review.PlatformMeta, "chat_id")
	if !ok || chatID != -1003615540583 {
		t.Fatalf("chat_id not preserved: %+v", review.PlatformMeta)
	}
	messageID, ok := metaInt(review.PlatformMeta, "message_id")
	if !ok || messageID != 21 {
		t.Fatalf("message_id not preserved: %+v", review.PlatformMeta)
	}

	tool, args, err := buildPlatformReply(review, "Спасибо за отзыв!")
	if err != nil {
		t.Fatalf("reply build failed after ingestion: %v", err)
	}
	if tool != tools.TelegramReplyToComment {
		t.Errorf("tool = %q", tool)
	}
	if args["chat_id"].(string) != "-1003615540583" {
		t.Errorf("chat_id = %v", args["chat_id"])
	}
	if args["message_id"].(float64) != 21 {
		t.Errorf("message_id = %v", args["message_id"])
	}
}

// VK comments address replies via external_id (<post>_<comment>), not chat
// coordinates, so reviewFromMap must not invent a platform_meta for them.
func TestReviewFromMap_VKHasNoPlatformMeta(t *testing.T) {
	m := map[string]interface{}{
		"id":      float64(42),
		"post_id": float64(11),
		"from_id": float64(7),
		"text":    "комментарий",
		"date":    float64(1_700_000_000),
	}
	review := reviewFromMap(m, "biz-1", a2a.AgentVK)
	if review.PlatformMeta != nil {
		t.Errorf("expected nil PlatformMeta for VK, got %+v", review.PlatformMeta)
	}
}

func TestBuildPlatformReply_Yandex(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentYandexBusiness, ExternalID: "yreview-77"}
	tool, args, err := buildPlatformReply(r, "thx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.YandexBusinessReplyReview {
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
	if tool != tools.GoogleBusinessReplyReview {
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
