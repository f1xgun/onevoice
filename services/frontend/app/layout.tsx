import { cookies } from 'next/headers';
import { THEME_COOKIE, resolveTheme } from '@/lib/theme';
import { ThemeProvider } from '@/components/design-system/ThemeProvider';
import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Golos_Text, JetBrains_Mono } from 'next/font/google';
import { getLocale, getMessages, getTranslations } from 'next-intl/server';
import './globals.css';
import { Providers } from '@/components/providers';
import { IntlClientProvider } from '@/components/IntlClientProvider';
import { SkipLink } from '@/components/a11y/skip-link';

const sans = Golos_Text({
  subsets: ['latin', 'cyrillic'],
  weight: ['400', '500', '600'],
  variable: '--font-sans',
  display: 'swap',
});
const mono = JetBrains_Mono({
  subsets: ['latin', 'cyrillic'],
  weight: '400',
  preload: false,
  variable: '--font-mono',
  display: 'swap',
});

// `generateMetadata` runs per-request on the server so the document
// title and description follow whichever locale the resolver in
// `lib/i18n/request.ts` selected (cookie → Accept-Language → 'ru').
// Strings live under the `metadata` namespace in `messages/{ru,en}.json`.
export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('metadata');
  return {
    title: t('title'),
    description: t('description'),
  };
}

export default async function RootLayout({ children }: { children: ReactNode }) {
  const theme = resolveTheme((await cookies()).get(THEME_COOKIE)?.value);
  const locale = await getLocale();
  const messages = await getMessages();

  return (
    <html
      lang={locale}
      data-ov-motion
      className={`${sans.variable} ${mono.variable} ${theme === 'system' ? '' : theme} scroll-pb-32 scroll-pt-24 md:scroll-pb-8 md:scroll-pt-28`}
    >
      <body className="font-sans antialiased">
        <IntlClientProvider locale={locale} messages={messages}>
          <SkipLink />
          <ThemeProvider theme={theme}>
            <Providers>{children}</Providers>
          </ThemeProvider>
        </IntlClientProvider>
      </body>
    </html>
  );
}
