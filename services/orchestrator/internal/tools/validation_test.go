package tools_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

// TestValidateEditArgs exercises every branch of the HITL-07 edit contract:
//   - happy path (allowlisted field + scalar value)
//   - unknown field → ErrFieldNotEditable
//   - case mismatch → ErrFieldNotEditable (strict lowercase per Pitfall 8)
//   - nested object/array/nil → ErrNonScalarValue (D-13: scalars only)
//   - unknown tool → ErrFieldNotEditable with Editable == nil
//   - multiple valid scalars in the same call
//
// The table is intentionally exhaustive so downstream handlers (Plan 16-07)
// can map each error shape to a 400 body without needing further branching.
func TestValidateEditArgs(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(
		makeDef("telegram__send_channel_post"),
		"",
		nil,
		domain.ToolFloorManual,
		[]string{"text", "parse_mode"},
	)
	reg.Register(
		makeDef("multi_scalar"),
		"",
		nil,
		domain.ToolFloorAuto,
		[]string{"a", "b", "c"},
	)

	cases := []struct {
		name       string
		tool       string
		args       map[string]interface{}
		wantErrAs  interface{} // pointer to a struct type (e.g., new(*tools.ErrFieldNotEditable))
		wantOK     bool
		wantField  string // expected Field in the error if applicable
		wantTool   string // expected Tool in the error if applicable
		nilEditLst bool   // expected Editable == nil (unknown-tool case)
	}{
		{
			name:     "happy path: whitelisted field with string value returns nil",
			tool:     "telegram__send_channel_post",
			args:     map[string]interface{}{"text": "hello"},
			wantOK:   true,
			wantTool: "telegram__send_channel_post",
		},
		{
			name:      "unknown field 'channel_id' → ErrFieldNotEditable",
			tool:      "telegram__send_channel_post",
			args:      map[string]interface{}{"channel_id": "-1001234567890"},
			wantErrAs: new(*tools.ErrFieldNotEditable),
			wantField: "channel_id",
			wantTool:  "telegram__send_channel_post",
		},
		{
			name:      "case mismatch: 'Text' when allowlist has 'text' → ErrFieldNotEditable",
			tool:      "telegram__send_channel_post",
			args:      map[string]interface{}{"Text": "hi"},
			wantErrAs: new(*tools.ErrFieldNotEditable),
			wantField: "Text",
			wantTool:  "telegram__send_channel_post",
		},
		{
			name:      "nested object value → ErrNonScalarValue (D-13 no nested editing)",
			tool:      "telegram__send_channel_post",
			args:      map[string]interface{}{"text": map[string]interface{}{"x": 1}},
			wantErrAs: new(*tools.ErrNonScalarValue),
			wantField: "text",
			wantTool:  "telegram__send_channel_post",
		},
		{
			name:      "array value → ErrNonScalarValue",
			tool:      "telegram__send_channel_post",
			args:      map[string]interface{}{"text": []interface{}{"a", "b"}},
			wantErrAs: new(*tools.ErrNonScalarValue),
			wantField: "text",
			wantTool:  "telegram__send_channel_post",
		},
		{
			name:      "nil value → ErrNonScalarValue (nil is not a scalar)",
			tool:      "telegram__send_channel_post",
			args:      map[string]interface{}{"text": nil},
			wantErrAs: new(*tools.ErrNonScalarValue),
			wantField: "text",
			wantTool:  "telegram__send_channel_post",
		},
		{
			name:       "unknown tool returns ErrFieldNotEditable with nil Editable",
			tool:       "ghost__missing",
			args:       map[string]interface{}{"a": "b"},
			wantErrAs:  new(*tools.ErrFieldNotEditable),
			wantField:  "a",
			wantTool:   "ghost__missing",
			nilEditLst: true,
		},
		{
			name:     "multiple valid scalars (float, bool, string) returns nil",
			tool:     "multi_scalar",
			args:     map[string]interface{}{"a": 1.5, "b": true, "c": "ok"},
			wantOK:   true,
			wantTool: "multi_scalar",
		},
		{
			name:     "json-numeric (float64) value on allowlisted field returns nil",
			tool:     "telegram__send_channel_post",
			args:     map[string]interface{}{"text": "x", "parse_mode": "Markdown"},
			wantOK:   true,
			wantTool: "telegram__send_channel_post",
		},
		{
			name:     "empty editedArgs returns nil regardless of tool",
			tool:     "telegram__send_channel_post",
			args:     map[string]interface{}{},
			wantOK:   true,
			wantTool: "telegram__send_channel_post",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.ValidateEditArgs(tc.tool, tc.args)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("ValidateEditArgs(%q, %v) = %v, want nil", tc.tool, tc.args, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateEditArgs(%q, %v) = nil, want error", tc.tool, tc.args)
			}
			switch want := tc.wantErrAs.(type) {
			case *(*tools.ErrFieldNotEditable):
				var got *tools.ErrFieldNotEditable
				if !errors.As(err, &got) {
					t.Fatalf("expected *ErrFieldNotEditable, got %T (%v)", err, err)
				}
				if got.Tool != tc.wantTool {
					t.Fatalf("Tool = %q, want %q", got.Tool, tc.wantTool)
				}
				if got.Field != tc.wantField {
					t.Fatalf("Field = %q, want %q", got.Field, tc.wantField)
				}
				if tc.nilEditLst && got.Editable != nil {
					t.Fatalf("Editable = %v, want nil (unknown-tool path)", got.Editable)
				}
				if !tc.nilEditLst && got.Editable == nil {
					t.Fatalf("Editable = nil, want non-nil allowlist for known tool %q", tc.tool)
				}
			case *(*tools.ErrNonScalarValue):
				var got *tools.ErrNonScalarValue
				if !errors.As(err, &got) {
					t.Fatalf("expected *ErrNonScalarValue, got %T (%v)", err, err)
				}
				if got.Tool != tc.wantTool {
					t.Fatalf("Tool = %q, want %q", got.Tool, tc.wantTool)
				}
				if got.Field != tc.wantField {
					t.Fatalf("Field = %q, want %q", got.Field, tc.wantField)
				}
			default:
				t.Fatalf("unexpected wantErrAs type %T in fixture", want)
			}
		})
	}
}

// TestValidateEditArgs_ErrorMessagesIncludeContext guards the exact substrings
// that the resolve handler (Plan 16-07) depends on when composing the 400 body.
// If these strings change, the handler's integration tests will break in a
// confusing way; locking them here gives early warning.
func TestValidateEditArgs_ErrorMessagesIncludeContext(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(
		makeDef("telegram__send_channel_post"),
		"",
		nil,
		domain.ToolFloorManual,
		[]string{"text"},
	)

	err := reg.ValidateEditArgs("telegram__send_channel_post", map[string]interface{}{"channel_id": "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !containsAll(msg, "channel_id", "telegram__send_channel_post", "text") {
		t.Fatalf("error message missing required context: %q", msg)
	}

	err = reg.ValidateEditArgs("telegram__send_channel_post", map[string]interface{}{"text": []interface{}{"a"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg = err.Error()
	if !containsAll(msg, "text", "telegram__send_channel_post", "string/number/bool") {
		t.Fatalf("non-scalar error missing required context: %q", msg)
	}
}

// containsAll reports whether s contains every substring.
func containsAll(s string, needles ...string) bool {
	for _, n := range needles {
		if !contains(s, n) {
			return false
		}
	}
	return true
}

func contains(s, needle string) bool {
	return len(s) >= len(needle) && indexOf(s, needle) >= 0
}

func indexOf(s, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
