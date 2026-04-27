'use client';

import { memo } from 'react';
import JsonViewEditor from '@uiw/react-json-view/editor';

// Shape of the `option` argument passed to @uiw/react-json-view/editor's
// `onEdit` callback. Mirrors the library's upstream type so tests can import
// it directly without depending on internal library types.
export interface JsonEditOption {
  value: unknown;
  oldValue: unknown;
  keyName?: string | number;
  parentName?: string | number;
  type?: 'value' | 'key';
}

export interface ToolApprovalJsonEditorProps {
  /** Persisted server args — read-only source. */
  args: Record<string, unknown>;
  /** Current draft overrides — only top-level scalars the user has changed. */
  editedArgs: Record<string, string | number | boolean>;
  /** Per-tool whitelist from the SSE event's `editable_fields`. */
  editableFields: string[];
  /** Called with `(key, value)` for every accepted top-level scalar edit. */
  onEdit: (key: string, value: string | number | boolean) => void;
}

// Bridge the @uiw/react-json-view theme to shadcn's neutral palette. Only two
// CSS variables are overridden per UI-SPEC §JSON Editor Contract; everything
// else uses the library's readable defaults on `--muted`. The `React.CSSProperties`
// cast is required because TypeScript does not type custom CSS variables natively.
const jsonEditorTheme = {
  '--w-rjv-color': 'hsl(var(--foreground))',
  '--w-rjv-background-color': 'hsl(var(--muted))',
} as React.CSSProperties;

/**
 * Pure gate deciding whether `@uiw/react-json-view/editor` should accept the
 * edit described by `option`. Exported separately from the React component so
 * every branch can be exercised by unit tests without mounting the editor.
 *
 * The four gates are applied in order:
 *   1. Key renames (type === 'key') → reject unconditionally (UI-09).
 *   2. Nested edits (parentName set and non-empty) → reject (HITL-07, Pitfall 3).
 *   3. Non-scalar values (not string/number/boolean) → reject (HITL-L4).
 *   4. keyName must be in editableFields → otherwise reject (D-15 per-tool
 *      whitelist).
 *
 * Returning `true` tells the editor to apply the edit; `false` vetoes it.
 */
export function evaluateEditGate(option: JsonEditOption, editableFields: string[]): boolean {
  // (1) Key renames are never allowed.
  if (option.type === 'key') {
    return false;
  }
  // (2) Root-only policy. `parentName === undefined` OR empty string counts as
  //     the root node; anything else is a nested path and must be rejected
  //     even if the nested key happens to collide with a top-level allowlist
  //     entry (Pitfall 3).
  if (option.parentName !== undefined && option.parentName !== '') {
    return false;
  }
  // (3) Scalar-only. null has typeof 'object' and is rejected here.
  const t = typeof option.value;
  if (t !== 'string' && t !== 'number' && t !== 'boolean') {
    return false;
  }
  // (4) Per-tool whitelist — the final gate.
  const key = String(option.keyName ?? '');
  return editableFields.includes(key);
}

/**
 * `ToolApprovalJsonEditor` wraps `@uiw/react-json-view/editor` with the
 * four-gate edit whitelist. Pure component: no reducer state, no toasts —
 * the parent (Plan 17-04's `ToolApprovalAccordionEntry`) owns `editedArgs`
 * and surfaces UX feedback.
 */
export const ToolApprovalJsonEditor = memo(function ToolApprovalJsonEditor({
  args,
  editedArgs,
  editableFields,
  onEdit,
}: ToolApprovalJsonEditorProps) {
  // Merge: start from the persisted server args, overlay the user's draft.
  // Stable identity is irrelevant — JsonViewEditor re-renders on shallow
  // equality changes and does not depend on referential stability here.
  const value = { ...args, ...editedArgs };

  return (
    <JsonViewEditor
      value={value}
      editable
      collapsed={2}
      style={jsonEditorTheme}
      onEdit={(opt) => {
        const option: JsonEditOption = {
          value: opt.value,
          oldValue: opt.oldValue,
          keyName: opt.keyName,
          parentName: opt.parentName,
          type: opt.type,
        };
        if (!evaluateEditGate(option, editableFields)) {
          return false;
        }
        const key = String(option.keyName ?? '');
        onEdit(key, option.value as string | number | boolean);
        return true;
      }}
    />
  );
});
