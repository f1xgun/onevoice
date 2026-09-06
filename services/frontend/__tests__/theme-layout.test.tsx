import { renderToStaticMarkup } from 'react-dom/server';
import { expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import RootLayout from '@/app/layout';
const preference = vi.hoisted(() => ({ value: undefined as string | undefined }));
vi.mock('next/headers', () => ({
  cookies: async () => ({ get: () => ({ value: preference.value }) }),
}));
vi.mock('next/font/google', () => ({
  Golos_Text: () => ({ variable: 'sans' }),
  JetBrains_Mono: () => ({ variable: 'mono' }),
}));
vi.mock('next-intl/server', () => ({
  getLocale: async () => 'ru',
  getMessages: async () => ({}),
  getTranslations: async () => (key: string) => key,
}));
vi.mock('@/components/providers', () => ({
  Providers: ({ children }: { children: ReactNode }) => children,
}));
vi.mock('@/components/IntlClientProvider', () => ({
  IntlClientProvider: ({ children }: { children: ReactNode }) => children,
}));

it.each([
  ['dark', 'dark'],
  ['light', 'light'],
  ['system', null],
  [undefined, null],
  ['broken', null],
])('server renders cookie %s as %s', async (cookie, expected) => {
  preference.value = cookie ?? undefined;
  const html = renderToStaticMarkup(await RootLayout({ children: <main>content</main> }));
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  expect(parsed.documentElement.classList.contains('dark')).toBe(expected === 'dark');
  expect(parsed.documentElement.classList.contains('light')).toBe(expected === 'light');
  expect(parsed.documentElement.lang).toBe('ru');
  expect(parsed.querySelector('script')).toBeNull();
});
