import { describe, it, expect } from 'vitest';
import { evaluateEditGate, type JsonEditOption } from '../ToolApprovalJsonEditor';

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

describe('evaluateEditGate — nested / type / rename rejection', () => {
  it('C) rejects a nested edit even when keyName is in editableFields', () => {
    // Payload shape from nestedArgsBatch fixture: { text: "top", meta: { text: "nested" } }.
    // editableFields = ["text"] — root "text" is editable, but meta.text must be denied.
    expect(
      evaluateEditGate(valueOption({ keyName: 'text', value: 'edited', parentName: 'meta' }), [
        'text',
      ])
    ).toBe(false);
  });

  it('D) rejects key renames unconditionally (type === "key")', () => {
    // Even if every other gate would accept, type=key is always denied (UI-09).
    expect(
      evaluateEditGate(
        { value: 'newKey', oldValue: 'text', keyName: 'text', parentName: undefined, type: 'key' },
        ['text']
      )
    ).toBe(false);
  });

  it('E) rejects a non-scalar object value', () => {
    expect(
      evaluateEditGate(valueOption({ keyName: 'text', value: { nested: 'x' } }), ['text'])
    ).toBe(false);
  });

  it('F) rejects a null value (typeof null === "object" — must be denied)', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'text', value: null }), ['text'])).toBe(false);
  });

  it('G) rejects an array value', () => {
    expect(evaluateEditGate(valueOption({ keyName: 'text', value: [1, 2] }), ['text'])).toBe(false);
  });
});
