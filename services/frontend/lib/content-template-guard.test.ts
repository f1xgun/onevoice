import { describe, expect, it } from 'vitest';
import { shouldApplyRenderedTemplate } from './content-template-guard';

const base = {
  mounted: true,
  requestedBusinessId: 'a',
  currentBusinessId: 'a',
  requestedConversationKey: 'chat-1',
  currentConversationKey: 'chat-1',
  inputBeforeRequest: 'draft',
  currentInput: 'draft',
  disabled: false,
};

describe('shouldApplyRenderedTemplate', () => {
  it('accepts an unchanged active composer', () =>
    expect(shouldApplyRenderedTemplate(base)).toBe(true));
  it.each([
    ['organization changed', { currentBusinessId: 'b' }],
    ['conversation changed', { currentConversationKey: 'chat-2' }],
    ['user typed', { currentInput: 'new draft' }],
    ['composer became disabled', { disabled: true }],
    ['component unmounted', { mounted: false }],
  ])('rejects completion when %s', (_name, change) =>
    expect(shouldApplyRenderedTemplate({ ...base, ...change })).toBe(false)
  );
});
