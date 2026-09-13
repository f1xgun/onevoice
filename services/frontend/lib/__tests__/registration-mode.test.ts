import { describe, expect, it } from 'vitest';

import { parseRegistrationMode } from '@/lib/registration-mode';

describe('parseRegistrationMode', () => {
  it.each([undefined, '', '   '])('keeps local development open for %s', (value) => {
    expect(parseRegistrationMode(value)).toBe('open');
  });

  it('accepts an explicit open mode case-insensitively', () => {
    expect(parseRegistrationMode(' OPEN ')).toBe('open');
  });

  it.each(['invite_only', 'closed', 'typo'])('fails closed for %s', (value) => {
    expect(parseRegistrationMode(value)).toBe('invite_only');
  });
});
