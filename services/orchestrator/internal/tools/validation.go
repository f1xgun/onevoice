package tools

import "fmt"

// ErrFieldNotEditable is returned by ValidateEditArgs when the client tries
// to edit a field that is not in the tool's EditableFields allowlist.
//
// The Editable slice is included so HITL-07 handlers (Plan 16-07) can echo
// the valid allowlist back in a 400 response body — per D-12's
// "never silently ignore" contract.
type ErrFieldNotEditable struct {
	Tool     string
	Field    string
	Editable []string
}

func (e *ErrFieldNotEditable) Error() string {
	return fmt.Sprintf("field %q is not editable for tool %q (editable: %v)", e.Field, e.Tool, e.Editable)
}

// ErrNonScalarValue is returned by ValidateEditArgs when an edited value is
// not a top-level scalar (string / number / bool). D-13 restricts edit-args
// to top-level scalars only for v1.3; nested objects and arrays are rejected
// with a 400 (HITL-L3 nested-editing is deferred to v1.4+).
type ErrNonScalarValue struct {
	Tool  string
	Field string
	Value interface{}
}

func (e *ErrNonScalarValue) Error() string {
	return fmt.Sprintf("field %q on tool %q must be string/number/bool (got %T)", e.Field, e.Tool, e.Value)
}

// ValidateEditArgs enforces the HITL-07 edit contract against the registry's
// EditableFields allowlist.
//
// Contract:
//   - Every key in editedArgs MUST appear in EditableFields(toolName).
//     Comparison is case-sensitive; canonical form is lowercase_with_underscore
//     (Pitfall 8). Unknown or case-mismatched keys return ErrFieldNotEditable.
//   - Every value MUST be a top-level scalar: string, float64/float32,
//     int/int32/int64, or bool. JSON unmarshalling produces float64 for every
//     numeric literal (even integers) — the int branches are there only for
//     tests that construct edits programmatically. Anything else (maps, slices,
//     nil) is rejected with ErrNonScalarValue (D-13 — no nested editing in v1.3).
//   - For an unknown toolName, EditableFields returns nil and every field in
//     editedArgs is rejected with ErrFieldNotEditable.Editable == nil. This
//     matches POLICY-07: unknown tools behave as if everything is forbidden.
//
// Returns nil when every (field, value) pair passes both checks. No allocations
// on the happy path beyond the EditableFields defensive copy.
func (r *Registry) ValidateEditArgs(toolName string, editedArgs map[string]interface{}) error {
	editable := r.EditableFields(toolName) // nil for unknown tools → every field rejected
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
			// ok — top-level scalar
		default:
			return &ErrNonScalarValue{Tool: toolName, Field: field, Value: value}
		}
	}
	return nil
}
