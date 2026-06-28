package tools_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestValidateEditArgs_HappyPath(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"text": "hello"},
		[]string{"text", "parse_mode"},
	)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateEditArgs_UnknownField_ReturnsErrFieldNotEditable(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"channel_id": "-100"},
		[]string{"text", "parse_mode"},
	)
	var e *tools.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable, got %v", err)
	}
	if e.Field != "channel_id" {
		t.Errorf("field = %q, want channel_id", e.Field)
	}
	if e.Tool != tools.TelegramSendChannelPost {
		t.Errorf("tool = %q", e.Tool)
	}
	if len(e.Editable) != 2 || e.Editable[0] != "text" || e.Editable[1] != "parse_mode" {
		t.Errorf("editable = %v, want [text parse_mode]", e.Editable)
	}
}

func TestValidateEditArgs_CaseMismatch_ReturnsErrFieldNotEditable(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"Text": "hello"},
		[]string{"text"},
	)
	var e *tools.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for case mismatch, got %v", err)
	}
}

func TestValidateEditArgs_NestedObject_ReturnsErrNonScalarValue(t *testing.T) {
	err := tools.ValidateEditArgs("tool",
		map[string]interface{}{"text": map[string]interface{}{"nested": 1}},
		[]string{"text"},
	)
	var e *tools.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for nested object, got %v", err)
	}
}

func TestValidateEditArgs_Array_ReturnsErrNonScalarValue(t *testing.T) {
	err := tools.ValidateEditArgs("tool",
		map[string]interface{}{"text": []string{"a", "b"}},
		[]string{"text"},
	)
	var e *tools.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for array, got %v", err)
	}
}

func TestValidateEditArgs_NilValue_ReturnsErrNonScalarValue(t *testing.T) {
	err := tools.ValidateEditArgs("tool",
		map[string]interface{}{"text": nil},
		[]string{"text"},
	)
	var e *tools.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for nil, got %v", err)
	}
}

func TestValidateEditArgs_NilEditable_EveryFieldRejected(t *testing.T) {
	err := tools.ValidateEditArgs("unknown_tool",
		map[string]interface{}{"text": "hello"},
		nil,
	)
	var e *tools.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for unknown tool, got %v", err)
	}
	if e.Editable != nil {
		t.Errorf("editable should be nil, got %v", e.Editable)
	}
}

func TestValidateEditArgs_MultipleScalars_AllAccepted(t *testing.T) {
	err := tools.ValidateEditArgs("tool",
		map[string]interface{}{
			"text":    "hello",
			"count":   float64(5),
			"enabled": true,
		},
		[]string{"text", "count", "enabled"},
	)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestValidateEditArgs_StringFieldBool_ReturnsErrFieldTypeMismatch pins the
// declared-type gate: an editable field declared type:"string" (text) must
// reject a bool. Without the gate the bool passes the scalar check, is
// persisted, and the agent coerces req.Args["text"].(string) -> "" -> a
// silent no-op post that still reports transport success.
func TestValidateEditArgs_StringFieldBool_ReturnsErrFieldTypeMismatch(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"text": true},
		[]string{"text"},
	)
	var e *tools.ErrFieldTypeMismatch
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldTypeMismatch for bool on string field, got %v", err)
	}
	if e.Field != "text" {
		t.Errorf("field = %q, want text", e.Field)
	}
	if e.Want != "string" {
		t.Errorf("want kind = %q, want string", e.Want)
	}
	if e.Tool != tools.TelegramSendChannelPost {
		t.Errorf("tool = %q", e.Tool)
	}
}

func TestValidateEditArgs_StringFieldNumber_ReturnsErrFieldTypeMismatch(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"text": float64(42)},
		[]string{"text"},
	)
	var e *tools.ErrFieldTypeMismatch
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldTypeMismatch for number on string field, got %v", err)
	}
}

func TestValidateEditArgs_StringFieldString_Accepted(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"text": "hello"},
		[]string{"text"},
	)
	if err != nil {
		t.Fatalf("want nil for string on string field, got %v", err)
	}
}

func TestValidateEditArgs_HoursFieldBool_ReturnsErrFieldTypeMismatch(t *testing.T) {
	err := tools.ValidateEditArgs(tools.YandexBusinessUpdateHours,
		map[string]interface{}{"hours": false},
		[]string{"hours"},
	)
	var e *tools.ErrFieldTypeMismatch
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldTypeMismatch for bool on hours field, got %v", err)
	}
}

func TestEditableFieldKind(t *testing.T) {
	for _, field := range []string{"text", "caption", "description", "hours"} {
		kind, ok := tools.EditableFieldKind(field)
		if !ok {
			t.Errorf("field %q should be known", field)
		}
		if kind != "string" {
			t.Errorf("field %q kind = %q, want string", field, kind)
		}
	}
	if _, ok := tools.EditableFieldKind("channel_id"); ok {
		t.Errorf("channel_id is not an editable field; should be unknown")
	}
}

func TestValidateEditArgs_JSONNumericScalar_Accepted(t *testing.T) {
	err := tools.ValidateEditArgs("tool",
		map[string]interface{}{"count": float64(42)},
		[]string{"count"},
	)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateEditArgs_EmptyArgs_NoError(t *testing.T) {
	err := tools.ValidateEditArgs("tool", map[string]interface{}{}, []string{"text"})
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestValidateEditArgs_ToolNameFieldAttempt_Rejected is the pinning
// assertion: if a client tries to rewrite tool_name via edited_args (an
// attempted tool swap), it must be rejected because "tool_name" never appears
// in any tool's EditableFields allowlist.
func TestValidateEditArgs_ToolNameFieldAttempt_Rejected(t *testing.T) {
	err := tools.ValidateEditArgs(tools.TelegramSendChannelPost,
		map[string]interface{}{"tool_name": tools.TelegramSendChannelPhoto},
		[]string{"text"},
	)
	var e *tools.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for tool_name tamper attempt, got %v", err)
	}
	if e.Field != "tool_name" {
		t.Errorf("field = %q, want tool_name", e.Field)
	}
}
