package domain

import "testing"

func TestPlatformMetaFromMap(t *testing.T) {
	meta := PlatformMetaFromMap(map[string]interface{}{
		"chat_id":    float64(-1003615540583),
		"message_id": float64(21),
		"text":       "ignored",
	})
	if meta["chat_id"].(int64) != -1003615540583 {
		t.Errorf("chat_id = %v", meta["chat_id"])
	}
	if meta["message_id"].(int64) != 21 {
		t.Errorf("message_id = %v", meta["message_id"])
	}

	if PlatformMetaFromMap(map[string]interface{}{"text": "x"}) != nil {
		t.Error("expected nil meta when no coordinates present")
	}

	strMeta := PlatformMetaFromMap(map[string]interface{}{"chat_id": "-100", "message_id": "5"})
	if strMeta["chat_id"].(int64) != -100 || strMeta["message_id"].(int64) != 5 {
		t.Errorf("string coords not coerced: %+v", strMeta)
	}

	partial := PlatformMetaFromMap(map[string]interface{}{"chat_id": float64(7)})
	if _, ok := partial["message_id"]; ok {
		t.Errorf("message_id should be absent: %+v", partial)
	}
}
