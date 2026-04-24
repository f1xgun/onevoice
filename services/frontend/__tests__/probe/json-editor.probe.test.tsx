// WAVE 0 PROBE — to be deleted in Plan 17-04 after the whitelist gate is written.
//
// Purpose: prove that
//   (a) the `@uiw/react-json-view/editor` subpath resolves at runtime in the
//       Vitest/jsdom environment (mirrors the Next.js build gate from step 2a),
//   (b) the `onEdit` prop shape expected by Plan 17-04's ToolApprovalJsonEditor
//       gate is accepted without TS coercion.
//
// We do NOT drive keystrokes here. The component only fires `onEdit` when the
// user commits an in-place edit via the input-element that @uiw mounts on
// double-click; simulating that through jsdom is brittle and belongs in
// Plan 17-04's `ToolApprovalCard.edit.test.tsx` (fireEvent-driven). The goal of
// this probe is strictly the subpath-resolution + prop-shape ground truth so
// the whitelist gate ships with a calibrated `parentName` contract.
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import JsonViewEditor from '@uiw/react-json-view/editor';

interface EditOption {
  value: unknown;
  oldValue: unknown;
  keyName?: string | number;
  parentName?: string | number;
  type?: 'value' | 'key';
}

describe('Wave 0 probe: JsonViewEditor onEdit', () => {
  it('mounts with a nested payload and accepts an onEdit callback', () => {
    const captured: EditOption[] = [];
    const { container } = render(
      <JsonViewEditor
        value={{ text: 'top', meta: { text: 'nested', author: 'alice' } }}
        editable
        onEdit={(opt) => {
          captured.push(opt as EditOption);
          // eslint-disable-next-line no-console
          console.log('PROBE_RESULT:', JSON.stringify(opt));
          return false; // always cancel for probe — we're only reading shape.
        }}
      />
    );
    // The editor mounts; we are not driving keystrokes here, just proving
    // the subpath resolves at runtime + the onEdit prop is accepted.
    expect(container.querySelector('div')).not.toBeNull();
    // No edits fire during static render — captured remains empty. This is
    // recorded in the commit message so Plan 17-04 can expect fireEvent-driven
    // tests to be the first place the real PROBE_RESULT shape surfaces.
    expect(captured).toHaveLength(0);
  });

  it('mounts with a flat payload without throwing', () => {
    const { container } = render(
      <JsonViewEditor
        value={{ chat_id: 123, text: 'hello', parse_mode: 'MarkdownV2' }}
        editable
        onEdit={() => false}
      />
    );
    expect(container.querySelector('div')).not.toBeNull();
  });
});
