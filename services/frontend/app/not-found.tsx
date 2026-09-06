// app/not-found.tsx — OneVoice (Linen) global 404
//
// Next 13+ convention: a top-level `not-found.tsx` in the app router
// catches any URL that doesn't resolve to a route. The layout above
// (`app/layout.tsx`) renders the providers + html shell, so this file
// only paints the body content.
//
// Mirrors the "404 — страницы нет" panel from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// calm paper background, mono "ERROR · 404" caption, big graphite
// headline ("Такого здесь нет"), short paragraph, single primary
// button back to /chat. Brand voice — no "Oops!", no emoji.

import Link from 'next/link';
import { getTranslations } from 'next-intl/server';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { MonoLabel } from '@/components/ui/mono-label';

export default async function NotFound() {
  const t = await getTranslations('common.notFound');
  return (
    <main
      id="main-content"
      tabIndex={-1}
      className="flex min-h-screen w-full items-center justify-center bg-paper px-6 py-16 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
    >
      <div className="flex w-full max-w-[480px] flex-col items-center gap-5 rounded-lg border border-line bg-paper-raised px-8 py-20 text-center shadow-ov-1">
        <MonoLabel className="text-ink-soft">{t('errorLabel')}</MonoLabel>
        <div
          aria-hidden="true"
          className="font-mono text-[56px] leading-none tracking-[-0.02em] text-ink-faint"
        >
          404
        </div>
        <div>
          <h1 className="text-[22px] font-medium leading-tight tracking-[-0.01em] text-ink">
            {t('title')}
          </h1>
          <p className="mt-2 text-sm leading-relaxed text-ink-mid">{t('body')}</p>
        </div>
        <Button asChild variant="primary" size="md">
          <Link href="/chat">{t('homeLink')}</Link>
        </Button>
      </div>
    </main>
  );
}
