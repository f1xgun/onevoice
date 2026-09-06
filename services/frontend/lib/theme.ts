export const THEME_COOKIE = 'NEXT_THEME';
export const DEFAULT_THEME = 'system';
export const THEMES = [
  { value: 'system', label: 'system' },
  { value: 'light', label: 'light' },
  { value: 'dark', label: 'dark' },
] as const;
export type Theme = (typeof THEMES)[number]['value'];

export function isTheme(value: unknown): value is Theme {
  return THEMES.some((theme) => theme.value === value);
}

export function resolveTheme(value: unknown): Theme {
  return isTheme(value) ? value : DEFAULT_THEME;
}
