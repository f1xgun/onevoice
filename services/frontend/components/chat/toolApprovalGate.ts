// Pure edit-gate helpers shared by `ToolApprovalArgsForm` and the
// `ToolApprovalCard.edited-args` test. Extracted from the now-deleted
// `ToolApprovalJsonEditor.tsx` so the gate can be reused without pulling
// in any UI primitives or third-party renderers.

/**
 * Shape of the per-edit option object the legacy JSON editor passed to
 * `onEdit`. Kept as a stable contract because tests build options by hand
 * and exercise every gate branch (rename / nested / non-scalar / off-list).
 */
export interface JsonEditOption {
  value: unknown;
  oldValue: unknown;
  keyName?: string | number;
  parentName?: string | number;
  type?: 'value' | 'key';
}

/**
 * Decides whether an edit described by `option` is allowed. The four gates
 * are applied in order:
 *   1. Key renames (`type === 'key'`) → reject (UI-09).
 *   2. Nested edits (parentName set and non-empty) → reject (HITL-07, Pitfall 3).
 *   3. Non-scalar values (not string/number/boolean) → reject (HITL-L4).
 *  4. keyName must be in editableFields → otherwise reject ( per-tool
 *      whitelist).
 *
 * Returns `true` when the edit should be accepted, `false` otherwise.
 */
export function evaluateEditGate(option: JsonEditOption, editableFields: string[]): boolean {
  if (option.type === 'key') {
    return false;
  }
  if (option.parentName !== undefined && option.parentName !== '') {
    return false;
  }
  const t = typeof option.value;
  if (t !== 'string' && t !== 'number' && t !== 'boolean') {
    return false;
  }
  const key = String(option.keyName ?? '');
  return editableFields.includes(key);
}
