import { describe, it, expect } from 'vitest';

import { evaluateEditGate, type JsonEditOption } from '../toolApprovalGate';

// Helper — build a `value`-type edit option with sensible defaults so each
// test only states what it actually overrides.
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

describe('evaluateEditGate — nested / type / rename rejection', () => {
  it('C) rejects a nested edit even when keyName is in editableFields', () => {
    expect(
      evaluateEditGate(valueOption({ keyName: 'text', value: 'edited', parentName: 'meta' }), [
        'text',
      ])
    ).toBe(false);
  });

  it('D) rejects key renames unconditionally (type === "key")', () => {
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
