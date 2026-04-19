package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// stubResumer is a minimal Resumer implementation that lets tests drive the
// ResumeHandler's SSE emission without a live orchestrator. Behavior: the
// test builds a channel, pushes events, and closes it — ResumeHandler
// serializes each event as an SSE frame.
type stubResumer struct {
	fn func(ctx context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error)
}

func (s *stubResumer) Resume(ctx context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
	return s.fn(ctx, req)
}

// TestResumeHandler_MissingBatchID_Returns400 — empty batch_id query param
// is a 400 before any orchestrator call is made.
func TestResumeHandler_MissingBatchID_Returns400(t *testing.T) {
	h := handler.NewResumeHandler(&stubResumer{fn: func(_ context.Context, _ orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
		t.Fatal("resumer should not be called when batch_id is missing")
		return nil, nil
	}})

	req := httptest.NewRequest(http.MethodPost, "/chat/conv1/resume", http.NoBody)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestResumeHandler_StreamsEventsAsSSE — given a resumer that emits a pair of
// events (tool_result + done), the handler must write both as SSE `data:`
// frames to the response body.
func TestResumeHandler_StreamsEventsAsSSE(t *testing.T) {
	h := handler.NewResumeHandler(&stubResumer{fn: func(_ context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
		if req.BatchID != "batch-123" {
			t.Fatalf("batch_id = %q, want batch-123", req.BatchID)
		}
		ch := make(chan orchestrator.Event, 2)
		ch <- orchestrator.Event{
			Type:       orchestrator.EventToolResult,
			ToolCallID: "toolu_abc",
			ToolName:   "telegram__send_channel_post",
			ToolResult: map[string]interface{}{"ok": true},
		}
		ch <- orchestrator.Event{Type: orchestrator.EventDone}
		close(ch)
		return ch, nil
	}})

	req := httptest.NewRequest(http.MethodPost, "/chat/conv1/resume?batch_id=batch-123", http.NoBody)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_result"`) {
		t.Errorf("body missing tool_result event: %q", body)
	}
	if !strings.Contains(body, `"type":"done"`) {
		t.Errorf("body missing done event: %q", body)
	}
	if !strings.Contains(body, `"tool_call_id":"toolu_abc"`) {
		t.Errorf("body missing tool_call_id: %q", body)
	}
}

// TestResumeHandler_ForwardsFreshApprovalMaps — the body carries business +
// project approval maps; the handler must pass them through verbatim in the
// ResumeRequest.
func TestResumeHandler_ForwardsFreshApprovalMaps(t *testing.T) {
	var got orchestrator.ResumeRequest
	h := handler.NewResumeHandler(&stubResumer{fn: func(_ context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
		got = req
		ch := make(chan orchestrator.Event, 1)
		ch <- orchestrator.Event{Type: orchestrator.EventDone}
		close(ch)
		return ch, nil
	}})

	body := `{
		"business_approvals": {"telegram__send_channel_post": "manual"},
		"project_approval_overrides": {"vk__publish_post": "forbidden"},
		"active_integrations": ["telegram","vk"],
		"whitelist_mode": "all",
		"allowed_tools": ["a","b"],
		"model": "gpt-5",
		"tier": "pro"
	}`
	req := httptest.NewRequest(http.MethodPost, "/chat/conv1/resume?batch_id=b1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.BatchID != "b1" {
		t.Errorf("BatchID = %q, want b1", got.BatchID)
	}
	if got.BusinessApprovals["telegram__send_channel_post"] != domain.ToolFloorManual {
		t.Errorf("business approvals not forwarded: %v", got.BusinessApprovals)
	}
	if got.ProjectApprovalOverrides["vk__publish_post"] != domain.ToolFloorForbidden {
		t.Errorf("project overrides not forwarded: %v", got.ProjectApprovalOverrides)
	}
	if got.Model != "gpt-5" {
		t.Errorf("Model = %q, want gpt-5", got.Model)
	}
}

// TestResumeHandler_EmptyBody_UsesZeroValues — the implicit-resume path from
// chat_proxy sends http.NoBody. Handler must gracefully proceed with empty
// maps and still drive the orchestrator.
func TestResumeHandler_EmptyBody_UsesZeroValues(t *testing.T) {
	var called bool
	h := handler.NewResumeHandler(&stubResumer{fn: func(_ context.Context, req orchestrator.ResumeRequest) (<-chan orchestrator.Event, error) {
		called = true
		if req.BatchID != "b1" {
			t.Errorf("BatchID = %q, want b1", req.BatchID)
		}
		ch := make(chan orchestrator.Event, 1)
		ch <- orchestrator.Event{Type: orchestrator.EventDone}
		close(ch)
		return ch, nil
	}})
	req := httptest.NewRequest(http.MethodPost, "/chat/conv1/resume?batch_id=b1", http.NoBody)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("resumer was not invoked")
	}
}

// TestInternalToolsAll_ReturnsFullProjection exercises GET /internal/tools —
// the endpoint the API's GET /api/v1/tools passthrough consumes.
func TestInternalToolsAll_ReturnsFullProjection(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("vk__publish_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("get_reviews"), "", nil, domain.ToolFloorAuto, nil)

	h := handler.NewInternalToolsAllHandler(reg)
	req := httptest.NewRequest(http.MethodGet, "/internal/tools", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	raw, _ := io.ReadAll(rec.Body)
	var entries []map[string]interface{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(raw))
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), entries)
	}
	// Every entry must carry the 5 canonical keys.
	required := []string{"name", "platform", "floor", "editableFields", "description"}
	for i, e := range entries {
		for _, k := range required {
			if _, ok := e[k]; !ok {
				t.Fatalf("entry[%d] missing key %q: %v", i, k, e)
			}
		}
		// editableFields must be an array, never null.
		if _, ok := e["editableFields"].([]interface{}); !ok {
			t.Fatalf("entry[%d].editableFields is not an array: %v", i, e["editableFields"])
		}
	}
}
