package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

func makeDef(name string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: name, Description: "test", Parameters: map[string]interface{}{}},
	}
}

// TestInternalToolsNames_ReturnsRegistrySnapshot is the minimum contract the
// API service's POLICY-07 sweep relies on: every registered tool's name must
// appear in the JSON `names` array. Order-independent — the registry's Go
// map iteration is non-deterministic.
func TestInternalToolsNames_ReturnsRegistrySnapshot(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_channel_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("vk__publish_post"), "", nil, domain.ToolFloorManual, []string{"text"})
	reg.Register(makeDef("get_business_info"), "", nil, domain.ToolFloorAuto, nil)

	h := handler.NewInternalToolsHandler(reg)
	req := httptest.NewRequest(http.MethodGet, "/internal/tools/names", http.NoBody)
	rec := httptest.NewRecorder()

	h.Names(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}

	got := make(map[string]struct{}, len(body.Names))
	for _, n := range body.Names {
		got[n] = struct{}{}
	}
	for _, want := range []string{"telegram__send_channel_post", "vk__publish_post", "get_business_info"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("names missing %q; got %v", want, body.Names)
		}
	}
	if len(body.Names) != 3 {
		t.Fatalf("len(names) = %d, want 3; got %v", len(body.Names), body.Names)
	}
}

// TestInternalToolsNames_EmptyRegistry exercises the edge where the
// orchestrator booted without NATS (tools not registered). The endpoint must
// still return a valid JSON shape so the API service's retry path does not
// error out — the sweep simply logs zero warnings.
func TestInternalToolsNames_EmptyRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	h := handler.NewInternalToolsHandler(reg)

	req := httptest.NewRequest(http.MethodGet, "/internal/tools/names", http.NoBody)
	rec := httptest.NewRecorder()
	h.Names(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if len(body.Names) != 0 {
		t.Fatalf("expected empty names, got %v", body.Names)
	}
}
