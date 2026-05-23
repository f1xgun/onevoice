package toolregistry_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// TestValidateEditArgs exercises every branch of the HITL edit contract:
//   - happy path (allowlisted field + scalar value)
//   - unknown field → ErrFieldNotEditable
//   - case mismatch → ErrFieldNotEditable (strict lowercase)
//   - nested object/array/nil → ErrNonScalarValue (scalars only)
//   - unknown tool → ErrFieldNotEditable with Editable == nil
//   - multiple valid scalars in the same call
//
// The table is intentionally exhaustive so downstream handlers
// can map each error shape to a 400 body without needing further branching.
func TestValidateEditArgs(t *testing.T) {
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{
		Def:            makeDef(tools.TelegramSendChannelPost),
		Floor:          domain.ToolFloorManual,
		EditableFields: []string{"text", "parse_mode"},
	}, nil)
	reg.Register(toolregistry.ToolSpec{
		Def:            makeDef("multi_scalar"),
		Floor:          domain.ToolFloorAuto,
		EditableFields: []string{"a", "b", "c"},
	}, nil)

	cases := []struct {
		name       string
		tool       string
		args       map[string]interface{}
		wantErrAs  interface{} // pointer to a struct type (e.g., new(*toolregistry.ErrFieldNotEditable))
		wantOK     bool
		wantField  string // expected Field in the error if applicable
		wantTool   string // expected Tool in the error if applicable
		nilEditLst bool   // expected Editable == nil (unknown-tool case)
	}{
		{
			name:     "happy path: whitelisted field with string value returns nil",
			tool:     tools.TelegramSendChannelPost,
			args:     map[string]interface{}{"text": "hello"},
			wantOK:   true,
			wantTool: tools.TelegramSendChannelPost,
		},
		{
			name:      "unknown field 'channel_id' → ErrFieldNotEditable",
			tool:      tools.TelegramSendChannelPost,
			args:      map[string]interface{}{"channel_id": "-1001234567890"},
			wantErrAs: new(*toolregistry.ErrFieldNotEditable),
			wantField: "channel_id",
			wantTool:  tools.TelegramSendChannelPost,
		},
		{
			name:      "case mismatch: 'Text' when allowlist has 'text' → ErrFieldNotEditable",
			tool:      tools.TelegramSendChannelPost,
			args:      map[string]interface{}{"Text": "hi"},
			wantErrAs: new(*toolregistry.ErrFieldNotEditable),
			wantField: "Text",
			wantTool:  tools.TelegramSendChannelPost,
		},
		{
			name:      "nested object value → ErrNonScalarValue (no nested editing)",
			tool:      tools.TelegramSendChannelPost,
			args:      map[string]interface{}{"text": map[string]interface{}{"x": 1}},
			wantErrAs: new(*toolregistry.ErrNonScalarValue),
			wantField: "text",
			wantTool:  tools.TelegramSendChannelPost,
		},
		{
			name:      "array value → ErrNonScalarValue",
			tool:      tools.TelegramSendChannelPost,
			args:      map[string]interface{}{"text": []interface{}{"a", "b"}},
			wantErrAs: new(*toolregistry.ErrNonScalarValue),
			wantField: "text",
			wantTool:  tools.TelegramSendChannelPost,
		},
		{
			name:      "nil value → ErrNonScalarValue (nil is not a scalar)",
			tool:      tools.TelegramSendChannelPost,
			args:      map[string]interface{}{"text": nil},
			wantErrAs: new(*toolregistry.ErrNonScalarValue),
			wantField: "text",
			wantTool:  tools.TelegramSendChannelPost,
		},
		{
			name:       "unknown tool returns ErrFieldNotEditable with nil Editable",
			tool:       "ghost__missing",
			args:       map[string]interface{}{"a": "b"},
			wantErrAs:  new(*toolregistry.ErrFieldNotEditable),
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
			tool:     tools.TelegramSendChannelPost,
			args:     map[string]interface{}{"text": "x", "parse_mode": "Markdown"},
			wantOK:   true,
			wantTool: tools.TelegramSendChannelPost,
		},
		{
			name:     "empty editedArgs returns nil regardless of tool",
			tool:     tools.TelegramSendChannelPost,
			args:     map[string]interface{}{},
			wantOK:   true,
			wantTool: tools.TelegramSendChannelPost,
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
			case **toolregistry.ErrFieldNotEditable:
				var got *toolregistry.ErrFieldNotEditable
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
			case **toolregistry.ErrNonScalarValue:
				var got *toolregistry.ErrNonScalarValue
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
// that the resolve handler depends on when composing the 400 body.
// If these strings change, the handler's integration tests will break in a
// confusing way; locking them here gives early warning.
func TestValidateEditArgs_ErrorMessagesIncludeContext(t *testing.T) {
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{
		Def:            makeDef(tools.TelegramSendChannelPost),
		Floor:          domain.ToolFloorManual,
		EditableFields: []string{"text"},
	}, nil)

	err := reg.ValidateEditArgs(tools.TelegramSendChannelPost, map[string]interface{}{"channel_id": "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !containsAll(msg, "channel_id", tools.TelegramSendChannelPost, "text") {
		t.Fatalf("error message missing required context: %q", msg)
	}

	err = reg.ValidateEditArgs(tools.TelegramSendChannelPost, map[string]interface{}{"text": []interface{}{"a"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg = err.Error()
	if !containsAll(msg, "text", tools.TelegramSendChannelPost, "string/number/bool") {
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
	if needle == "" {
		return 0
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
