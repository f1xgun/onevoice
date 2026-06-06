import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Manrope, JetBrains_Mono } from 'next/font/google';
import { NextIntlClientProvider } from 'next-intl';
import { getLocale, getMessages, getTranslations } from 'next-intl/server';
import './globals.css';
import { Providers } from '@/components/providers';
import { SkipLink } from '@/components/a11y/skip-link';

// Manrope is the cyrillic-supporting fallback for Mona Sans (the design spec's
// preferred sans). Mona Sans on Google Fonts ships latin only — see
// design_handoff/tokens/PRODUCTION-README §1. Switch to self-hosted Mona Sans
// from github.com/github/mona-sans if cyrillic glyphs are added upstream.
const sans = Manrope({
  subsets: ['latin', 'cyrillic'],
  variable: '--font-sans',
  display: 'swap',
});
const mono = JetBrains_Mono({
  subsets: ['latin'],
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
  const locale = await getLocale();
  const messages = await getMessages();

  return (
    <html lang={locale} className={`${sans.variable} ${mono.variable}`}>
      <body className="font-sans antialiased">
        <NextIntlClientProvider locale={locale} messages={messages}>
          <SkipLink />
          <Providers>{children}</Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
