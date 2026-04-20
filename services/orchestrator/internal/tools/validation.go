package tools

import "github.com/f1xgun/onevoice/pkg/toolvalidation"

// ErrFieldNotEditable re-exports pkg/toolvalidation.ErrFieldNotEditable so
// legacy orchestrator callers continue to compile. The canonical type lives
// in pkg/ so that services/api can import the exact same type for the resolve
// handler's 400 body (D-12 — never silently ignore).
type ErrFieldNotEditable = toolvalidation.ErrFieldNotEditable

// ErrNonScalarValue re-exports pkg/toolvalidation.ErrNonScalarValue for the
// same reason as ErrFieldNotEditable.
type ErrNonScalarValue = toolvalidation.ErrNonScalarValue

// ValidateEditArgs delegates to pkg/toolvalidation.ValidateEditArgs after
// fetching the per-tool EditableFields allowlist from the registry. The
// underlying validation logic lives in pkg/ so services/api shares the exact
// same semantics for HITL-07 edit enforcement — Pitfall 8 (case-sensitivity)
// cannot diverge between orchestrator and API.
//
// Plan 16-07 relocation note: ValidateEditArgs was originally defined in this
// file; it now delegates to pkg/toolvalidation. See pkg/toolvalidation/validate.go
// for the full contract.
func (r *Registry) ValidateEditArgs(toolName string, editedArgs map[string]interface{}) error {
	editable := r.EditableFields(toolName) // nil for unknown tools → every field rejected
	return toolvalidation.ValidateEditArgs(toolName, editedArgs, editable)
}
