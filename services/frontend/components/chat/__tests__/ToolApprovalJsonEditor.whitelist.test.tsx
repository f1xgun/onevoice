import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import {
  evaluateEditGate,
  ToolApprovalJsonEditor,
  type JsonEditOption,
} from '../ToolApprovalJsonEditor';
import {
  singleCallBatch,
  threeCallBatch,
  nestedArgsBatch,
  noEditableFieldsBatch,
} from '@/test-utils/pending-approval-fixtures';

// Helper — build a `value`-type edit option with sensible defaults.
function valueOption(overrides: Partial<JsonEditOption> = {}): JsonEditOption {
  return {
    value: 'new',
    oldValue: 'old',
    keyName: 'text',
    parentName: undefined,
    type: 'value',
    ...overrides,
  };
}

describe('evaluateEditGate — whitelist / scalar / root acceptance', () => {
  it('A) accepts a scalar root-level edit when key is in editableFields', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'text', value: 'new' }), ['text'])).toBe(true);
  });

  it('B) rejects a root-level edit when key is NOT in editableFields', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'chat_id', value: 123 }), ['text'])).toBe(false);
  });

  it('H) accepts a boolean value when its key is allowlisted', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'silent', value: true }), ['silent'])).toBe(
      true
    );
  });

  it('I) accepts a number value when its key is allowlisted', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'count', value: 42 }), ['count'])).toBe(true);
  });

  it('J) rejects every edit when editableFields is empty', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'text', value: 'x' }), [])).toBe(false);
    expect(evaluateEditGate(valueOption({ keyName: 'silent', value: true }), [])).toBe(false);
    expect(evaluateEditGate(valueOption({ keyName: 'count', value: 7 }), [])).toBe(false);
  });

  it('K) treats parentName === "" as root (empty-string parent accepted)', () => {
    expect(
      evaluateEditGate(valueOption({ keyName: 'text', value: 'new', parentName: '' }), ['text'])
    ).toBe(true);
  });
});

describe('ToolApprovalJsonEditor — mounts with every canonical fixture', () => {
  // L) The component must render without crashing for each PendingApproval
  // fixture — proves the `@uiw/react-json-view/editor` subpath resolves at
  // runtime and the component wiring accepts the fixture shape.
  const fixtures = [
    { name: 'singleCallBatch', batch: singleCallBatch },
    { name: 'threeCallBatch', batch: threeCallBatch },
    { name: 'nestedArgsBatch', batch: nestedArgsBatch },
    { name: 'noEditableFieldsBatch', batch: noEditableFieldsBatch },
  ];

  for (const { name, batch } of fixtures) {
    it(`L) mounts with ${name}`, () => {
      const call = batch.calls[0];
      const { container } = render(
        <ToolApprovalJsonEditor
          args={call.args}
          editedArgs={{}}
          editableFields={call.editableFields}
          onEdit={vi.fn()}
        />
      );
      expect(container.querySelector('div')).not.toBeNull();
    });
  }
});
