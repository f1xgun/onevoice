// Package sse owns the wire shape of the Server-Sent Events that flow
// orchestrator → api → frontend. It exists so the shape lives in one place
// instead of being re-stated by every emitter and decoder along the path.
//
// Field ordering matters: encoding/json marshals struct fields in source
// order, and the goldens in services/orchestrator/internal/handler pin the
// exact bytes. Reordering a field here will fail those tests — which is the
// intent: the wire contract is the test surface.
//
// omitempty on every optional field keeps legacy events (e.g. text / done)
// byte-identical to the pre-codec output so older clients keep parsing the
// same bytes after the refactor.
package sse

import (
	"encoding/json"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// Event is the JSON shape of a single SSE frame. The Type field discriminates
// which other fields are populated; see services/orchestrator/internal/orchestrator
// for the canonical EventType values (text / tool_call / tool_result /
// tool_rejected / tool_approval_required / done / error).
type Event struct {
	Type string `json:"type"`
	// Code is a machine-readable discriminator on error events so the api
	// proxy and frontend can branch on the failure mode without parsing
	// Content. omitempty keeps legacy events (text / done / non-coded
	// errors) byte-identical on the wire.
	Code               string                 `json:"code,omitempty"`
	Content            string                 `json:"content,omitempty"`
	ToolCallID         string                 `json:"tool_call_id,omitempty"`
	ToolName           string                 `json:"tool_name,omitempty"`
	ToolDisplayName    string                 `json:"tool_display_name,omitempty"`
	ToolDisplayNameKey string                 `json:"tool_display_name_key,omitempty"`
	ToolArgs           map[string]interface{} `json:"tool_args,omitempty"`
	ToolResult         interface{}            `json:"result,omitempty"`
	ToolError          string                 `json:"error,omitempty"`
	BatchID            string                 `json:"batch_id,omitempty"`
	Calls              []ApprovalCall         `json:"calls,omitempty"`
}

// ApprovalCall is the per-call projection bundled into a
// tool_approval_required event. EditableFields drives the UI's per-field
// read-only enforcement; Floor is always domain.ToolFloorManual for batched
// calls (forbidden calls never enter a batch — they get synthetic rejections
// instead).
//
// This is the canonical type for the orchestrator → api wire. The api → FE
// REST contract uses a different camelCase shape in services/api/internal/handler.
type ApprovalCall struct {
	CallID         string                 `json:"call_id"`
	ToolName       string                 `json:"tool_name"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editable_fields"`
	Floor          domain.ToolFloor       `json:"floor"`
}

// Marshal encodes an Event for an SSE `data:` frame. Thin wrapper over
// encoding/json so the seam has one place to grow validation / versioning
// later without touching callers.
func Marshal(e Event) ([]byte, error) {
	return json.Marshal(e)
}

// Unmarshal decodes the JSON payload of an SSE `data:` frame. Callers strip
// the `data: ` prefix and pass the remaining bytes.
func Unmarshal(b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}
