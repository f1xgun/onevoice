package domain

import "strconv"

// reviewMetaCoordKeys are the platform-coordinate fields preserved into a
// Review's PlatformMeta so a reply can target the original message. Telegram
// replies require chat_id+message_id; platforms that address replies via
// external_id (VK, Yandex, Google) contribute none and get a nil meta.
var reviewMetaCoordKeys = []string{"chat_id", "message_id"}

// PlatformMetaFromMap captures the reply-coordinate fields from a raw
// tool-result map into a Review.PlatformMeta. Values are coerced to int64 so
// they survive the agent → NATS (JSON float64) → BSON (long) round-trip and
// read back unchanged. Returns nil when no coordinates are present, so callers
// can assign the result directly and an empty meta is never persisted.
//
// Both review ingestion paths (the background syncer and the chat-turn
// tool-result fanout) call this so a Telegram review is repliable regardless of
// how it was first ingested.
func PlatformMetaFromMap(m map[string]interface{}) map[string]interface{} {
	meta := make(map[string]interface{}, len(reviewMetaCoordKeys))
	for _, key := range reviewMetaCoordKeys {
		if v, ok := metaCoordInt64(m[key]); ok {
			meta[key] = v
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func metaCoordInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case string:
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}
