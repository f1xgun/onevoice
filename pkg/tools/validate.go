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

// ErrFieldTypeMismatch is returned when an edited value is a scalar but does not
// match the field's declared type in the tool registry schema. Every editable
// field is declared type:"string" (see EditableFieldKind), so a bool/number
// supplied for such a field is rejected with a 400 instead of being silently
// coerced to "" by the agent (an approved-with-edit that posts nothing).
type ErrFieldTypeMismatch struct {
	Tool  string
	Field string
	Want  string
	Value interface{}
}

func (e *ErrFieldTypeMismatch) Error() string {
	return fmt.Sprintf("field %q on tool %q must be %s (got %T)", e.Field, e.Tool, e.Want, e.Value)
}

// editableFieldKind maps every editable field name (the union of all tools'
// EditableFields allowlists in the orchestrator registry) to its declared type
// in the tool's JSON-schema parameters. Sourced from the same registry specs
// that define the editable allowlists (services/orchestrator/internal/wire/
// tools_*.go): text, caption, description and hours are all declared
// type:"string". Keep this in sync when an editable field with a non-string
// declared type is added.
var editableFieldKind = map[string]string{
	"text":        "string",
	"caption":     "string",
	"description": "string",
	"hours":       "string",
}

// EditableFieldKind returns the declared type ("string", ...) for an editable
// field, and whether the field is known. Unknown fields return ("", false) and
// are not type-checked beyond the editable-allowlist and scalar gates — the
// allowlist (passed to ValidateEditArgs) is the authority on what may be edited.
func EditableFieldKind(field string) (string, bool) {
	k, ok := editableFieldKind[field]
	return k, ok
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
//   - Every value for a field with a declared type (see editableFieldKind)
//     MUST match that type. All editable fields are declared type:"string",
//     so a bool/number supplied for one returns ErrFieldTypeMismatch — without
//     this gate the agent coerces a non-string to "" and silently posts
//     nothing while still reporting transport success.
//
// Returns nil when every (field, value) pair passes the allowlist, scalar and
// declared-type checks. No allocations on the happy path beyond the allow-set
// map.
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
		if kind, known := editableFieldKind[field]; known {
			if err := assertKind(toolName, field, kind, value); err != nil {
				return err
			}
		}
	}
	return nil
}

// assertKind verifies value matches the declared kind for an editable field.
// Only "string" is enforced today (every editable field is declared string);
// other kinds pass through until a non-string editable field exists and is
// added to editableFieldKind with a matching branch here.
func assertKind(toolName, field, kind string, value interface{}) error {
	if kind != "string" {
		return nil
	}
	if _, ok := value.(string); !ok {
		return &ErrFieldTypeMismatch{Tool: toolName, Field: field, Want: kind, Value: value}
	}
	return nil
}
