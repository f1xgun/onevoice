package hitl_test

import (
	"reflect"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitl"
	"github.com/f1xgun/onevoice/pkg/llm"
)

// floorMap turns a static map into a FloorOf callback. Unknown tools default
// to ToolFloorForbidden, matching toolregistry.Registry.Floor's contract.
func floorMap(m map[string]domain.ToolFloor) hitl.FloorOf {
	return func(name string) domain.ToolFloor {
		if f, ok := m[name]; ok {
			return f
		}
		return domain.ToolFloorForbidden
	}
}

func toolCall(id, name string) llm.ToolCall {
	return llm.ToolCall{ID: id, Function: llm.FunctionCall{Name: name}}
}

func TestBucket_ClassifiesByEffectiveFloor(t *testing.T) {
	floors := floorMap(map[string]domain.ToolFloor{
		"telegram__send_channel_post": domain.ToolFloorAuto,
		"yandex__create_post":         domain.ToolFloorManual,
		"vk__delete_wall_post":        domain.ToolFloorForbidden,
	})
	calls := []llm.ToolCall{
		toolCall("c1", "telegram__send_channel_post"),
		toolCall("c2", "yandex__create_post"),
		toolCall("c3", "vk__delete_wall_post"),
	}

	auto, manual, forbidden := hitl.Bucket(floors, nil, nil, calls)

	if len(auto) != 1 || auto[0].ID != "c1" {
		t.Fatalf("auto bucket mismatch: %+v", auto)
	}
	if len(manual) != 1 || manual[0].ID != "c2" {
		t.Fatalf("manual bucket mismatch: %+v", manual)
	}
	if len(forbidden) != 1 || forbidden[0].ID != "c3" {
		t.Fatalf("forbidden bucket mismatch: %+v", forbidden)
	}
}

func TestBucket_BusinessApprovalRaisesFloor(t *testing.T) {
	// Registry floor is Auto, but business policy escalates to Manual —
	// the call must land in `manual`, mirroring the per-tool Resolve path.
	floors := floorMap(map[string]domain.ToolFloor{
		"telegram__send_channel_post": domain.ToolFloorAuto,
	})
	business := map[string]domain.ToolFloor{
		"telegram__send_channel_post": domain.ToolFloorManual,
	}

	_, manual, _ := hitl.Bucket(floors, business, nil, []llm.ToolCall{
		toolCall("c1", "telegram__send_channel_post"),
	})

	if len(manual) != 1 {
		t.Fatalf("expected business policy to escalate call to manual, got manual=%d", len(manual))
	}
}

func TestBucket_ProjectOverrideRaisesFloor(t *testing.T) {
	// Registry floor is Manual; project override escalates to Forbidden.
	floors := floorMap(map[string]domain.ToolFloor{
		"yandex__create_post": domain.ToolFloorManual,
	})
	project := map[string]domain.ToolFloor{
		"yandex__create_post": domain.ToolFloorForbidden,
	}

	_, _, forbidden := hitl.Bucket(floors, nil, project, []llm.ToolCall{
		toolCall("c1", "yandex__create_post"),
	})

	if len(forbidden) != 1 {
		t.Fatalf("expected project override to escalate call to forbidden, got forbidden=%d", len(forbidden))
	}
}

func TestBucket_UnknownToolBucketsForbidden(t *testing.T) {
	// Pre-extraction stepRun guard: unknown tools fall through to Forbidden
	// (Registry.Floor returns Forbidden for missing entries). Bucket must
	// preserve that fail-closed default.
	floors := floorMap(map[string]domain.ToolFloor{})

	_, _, forbidden := hitl.Bucket(floors, nil, nil, []llm.ToolCall{
		toolCall("c1", "unknown__action"),
	})

	if len(forbidden) != 1 {
		t.Fatalf("unknown tool must land in forbidden bucket, got forbidden=%d", len(forbidden))
	}
}

func TestBucket_EmptyCallsReturnsEmptyBuckets(t *testing.T) {
	auto, manual, forbidden := hitl.Bucket(floorMap(nil), nil, nil, nil)
	if auto != nil || manual != nil || forbidden != nil {
		t.Fatalf("nil input must return nil buckets, got auto=%v manual=%v forbidden=%v", auto, manual, forbidden)
	}
}

func TestBucket_PreservesInputOrderWithinBuckets(t *testing.T) {
	// Order within a single bucket must match input order — stepRun /
	// resume rely on this when appending tool-role messages so that
	// assistant.tool_calls[i].id ↔ tool[i] correspondence is preserved.
	floors := floorMap(map[string]domain.ToolFloor{
		"a__x": domain.ToolFloorAuto,
		"a__y": domain.ToolFloorAuto,
		"a__z": domain.ToolFloorAuto,
	})

	auto, _, _ := hitl.Bucket(floors, nil, nil, []llm.ToolCall{
		toolCall("c1", "a__x"),
		toolCall("c2", "a__y"),
		toolCall("c3", "a__z"),
	})

	gotIDs := []string{auto[0].ID, auto[1].ID, auto[2].ID}
	wantIDs := []string{"c1", "c2", "c3"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("order mismatch: got %v, want %v", gotIDs, wantIDs)
	}
}
