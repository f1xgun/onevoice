package tools

import "fmt"

// ErrFieldNotEditable is returned when the client tries to edit a field that
// is not in the tool's EditableFields allowlist.
//
// The Editable slice is included so handlers (services/api/internal/handler)
// can echo the valid allowlist back in a 400 response body — per the
// "never silently ignore" contract.
type ErrFieldNotEditable struct {
	Tool     string
	Field    string
	Editable []string
}

func (e *ErrFieldNotEditable) Error() string {
	return fmt.Sprintf("field %q is not editable for tool %q (editable: %v)", e.Field, e.Tool, e.Editable)
}

// ErrNonScalarValue is returned when an edited value is not a top-level scalar
// (string / number / bool). Edit-args are restricted to top-level scalars only
// for v1.3; nested objects and arrays are rejected with a 400 (nested-editing
// is deferred to v1.4+).
type ErrNonScalarValue struct {
	Tool  string
	Field string
	Value interface{}
}

func (e *ErrNonScalarValue) Error() string {
	return fmt.Sprintf("field %q on tool %q must be string/number/bool (got %T)", e.Field, e.Tool, e.Value)
}

// ValidateEditArgs enforces the edit contract against the supplied
// editable allowlist.
//
// Contract:
//   - Every key in editedArgs MUST appear in editable. Comparison is
//     case-sensitive; canonical form is lowercase_with_underscore.
//     Unknown or case-mismatched keys return ErrFieldNotEditable.
//   - Every value MUST be a top-level scalar: string, float64/float32,
//     int/int32/int64, or bool. JSON unmarshalling produces float64 for every
//     numeric literal (even integers) — the int branches are there only for
//     tests that construct edits programmatically. Anything else (maps, slices,
//     nil) is rejected with ErrNonScalarValue (no nested editing in v1.3).
//   - When editable is nil (e.g., unknown tool, or a tool with no editable
//     fields), every field in editedArgs is rejected with
//     ErrFieldNotEditable.Editable == nil. Unknown tools behave as if
//     everything is forbidden.
//
// Returns nil when every (field, value) pair passes both checks. No
// allocations on the happy path beyond the allow-set map.
//
// Historical note: this used to live in pkg/toolvalidation (split out to
// dodge Go's internal/ visibility wall between services/orchestrator and
// services/api). Both validator and tool-id constants share the same
// conceptual unit ("tool semantics importable across services"), so they
// now sit in one package — the dedicated split outlived its motivation.
func ValidateEditArgs(toolName string, editedArgs map[string]interface{}, editable []string) error {
	allow := make(map[string]struct{}, len(editable))
	for _, f := range editable {
		allow[f] = struct{}{}
	}
	for field, value := range editedArgs {
		if _, ok := allow[field]; !ok {
			return &ErrFieldNotEditable{Tool: toolName, Field: field, Editable: editable}
		}
		switch value.(type) {
		case string, float64, float32, int, int32, int64, bool:
		default:
			return &ErrNonScalarValue{Tool: toolName, Field: field, Value: value}
		}
	}
	return nil
}
