package toolvalidation_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/toolvalidation"
)

func TestValidateEditArgs_HappyPath(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("telegram__send_channel_post",
		map[string]interface{}{"text": "hello"},
		[]string{"text", "parse_mode"},
	)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateEditArgs_UnknownField_ReturnsErrFieldNotEditable(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("telegram__send_channel_post",
		map[string]interface{}{"channel_id": "-100"},
		[]string{"text", "parse_mode"},
	)
	var e *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable, got %v", err)
	}
	if e.Field != "channel_id" {
		t.Errorf("field = %q, want channel_id", e.Field)
	}
	if e.Tool != "telegram__send_channel_post" {
		t.Errorf("tool = %q", e.Tool)
	}
	if len(e.Editable) != 2 || e.Editable[0] != "text" || e.Editable[1] != "parse_mode" {
		t.Errorf("editable = %v, want [text parse_mode]", e.Editable)
	}
}

func TestValidateEditArgs_CaseMismatch_ReturnsErrFieldNotEditable(t *testing.T) {
	// Pitfall 8: case-sensitive matching. "Text" != "text".
	err := toolvalidation.ValidateEditArgs("telegram__send_channel_post",
		map[string]interface{}{"Text": "hello"},
		[]string{"text"},
	)
	var e *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for case mismatch, got %v", err)
	}
}

func TestValidateEditArgs_NestedObject_ReturnsErrNonScalarValue(t *testing.T) {
	// D-13: nested objects rejected.
	err := toolvalidation.ValidateEditArgs("tool",
		map[string]interface{}{"text": map[string]interface{}{"nested": 1}},
		[]string{"text"},
	)
	var e *toolvalidation.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for nested object, got %v", err)
	}
}

func TestValidateEditArgs_Array_ReturnsErrNonScalarValue(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("tool",
		map[string]interface{}{"text": []string{"a", "b"}},
		[]string{"text"},
	)
	var e *toolvalidation.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for array, got %v", err)
	}
}

func TestValidateEditArgs_NilValue_ReturnsErrNonScalarValue(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("tool",
		map[string]interface{}{"text": nil},
		[]string{"text"},
	)
	var e *toolvalidation.ErrNonScalarValue
	if !errors.As(err, &e) {
		t.Fatalf("want ErrNonScalarValue for nil, got %v", err)
	}
}

func TestValidateEditArgs_NilEditable_EveryFieldRejected(t *testing.T) {
	// Unknown tool path: Registry returns nil EditableFields.
	err := toolvalidation.ValidateEditArgs("unknown_tool",
		map[string]interface{}{"text": "hello"},
		nil,
	)
	var e *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for unknown tool, got %v", err)
	}
	if e.Editable != nil {
		t.Errorf("editable should be nil, got %v", e.Editable)
	}
}

func TestValidateEditArgs_MultipleScalars_AllAccepted(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("tool",
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

func TestValidateEditArgs_JSONNumericScalar_Accepted(t *testing.T) {
	// JSON decodes numbers as float64 — ensure that path is accepted.
	err := toolvalidation.ValidateEditArgs("tool",
		map[string]interface{}{"count": float64(42)},
		[]string{"count"},
	)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateEditArgs_EmptyArgs_NoError(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("tool", map[string]interface{}{}, []string{"text"})
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestValidateEditArgs_ToolNameFieldAttempt_Rejected is the HITL-07 pinning
// assertion: if a client tries to rewrite tool_name via edited_args (an
// attempted tool swap), it must be rejected because "tool_name" never appears
// in any tool's EditableFields allowlist.
func TestValidateEditArgs_ToolNameFieldAttempt_Rejected(t *testing.T) {
	err := toolvalidation.ValidateEditArgs("telegram__send_channel_post",
		map[string]interface{}{"tool_name": "telegram__send_channel_photo"},
		[]string{"text"},
	)
	var e *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &e) {
		t.Fatalf("want ErrFieldNotEditable for tool_name tamper attempt, got %v", err)
	}
	if e.Field != "tool_name" {
		t.Errorf("field = %q, want tool_name", e.Field)
	}
}
