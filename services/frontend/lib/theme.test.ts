import { describe, expect, it } from 'vitest';
import { isTheme, resolveTheme, THEMES, THEME_COOKIE } from './theme';

describe('theme preference', () => {
  it.each(THEMES)('accepts $value', ({ value }) => {
    expect(isTheme(value)).toBe(true);
    expect(resolveTheme(value)).toBe(value);
  });
  it.each([undefined, null, '', 'auto', 'Dark', {}, 1])(
    'defaults invalid %j to system',
    (value) => {
      expect(isTheme(value)).toBe(false);
      expect(resolveTheme(value)).toBe('system');
    }
  );
  it('uses the preference cookie', () => expect(THEME_COOKIE).toBe('NEXT_THEME'));
});
