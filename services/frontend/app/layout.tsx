import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Manrope, JetBrains_Mono } from 'next/font/google';
import './globals.css';
import { Providers } from '@/components/providers';

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

export const metadata: Metadata = {
  title: 'OneVoice — управление цифровым присутствием',
  description: 'Мультиагентная система для автоматизации SMB',
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="ru" className={`${sans.variable} ${mono.variable}`}>
      <body className="font-sans antialiased">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
