// __tests__/i18n-locale-switch.smoke.test.tsx
//
// End-to-end-ish proof that the next-intl mock in `vitest.setup.ts`
// honours a per-test locale flip. Default behaviour: every test pins
// to `ru` (consistent with the other ~39 component tests that assert
// RU literals). This file opts in to `en` via the global
// `__setTestLocale('en')` helper to prove the EN side of the bundle
// is loadable from inside a render.
//
// Why a tiny inline component instead of importing `app/page.tsx`:
// the landing page mounts many subcomponents (fonts, providers, font
// loader globals) that aren't pertinent to the assertion we want to
// make here — we just want to confirm that `useTranslations` reaches
// `messages/en.json` when the locale is flipped.

import { describe, expect, it, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { useLocale, useTranslations } from 'next-intl';

declare global {
  // eslint-disable-next-line no-var
  var __setTestLocale: (locale: 'ru' | 'en') => void;
}

function HeroProbe() {
  const t = useTranslations('landing.hero');
  const locale = useLocale();
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <h1 data-testid="headline">{t('headlineLine1')}</h1>
    </div>
  );
}

describe('i18n locale switch (smoke)', () => {
  afterEach(() => {
    // Belt-and-braces — the global afterEach in vitest.setup.ts also
    // resets locale to 'ru', but explicit local cleanup makes the
    // ordering obvious to future readers.
    globalThis.__setTestLocale('ru');
  });

  it('renders the RU headline by default', () => {
    render(<HeroProbe />);
    expect(screen.getByTestId('locale')).toHaveTextContent('ru');
    expect(screen.getByTestId('headline')).toHaveTextContent('Один разговор');
  });

  it('renders the EN headline after __setTestLocale("en")', () => {
    globalThis.__setTestLocale('en');
    render(<HeroProbe />);
    expect(screen.getByTestId('locale')).toHaveTextContent('en');
    expect(screen.getByTestId('headline')).toHaveTextContent('One conversation');
  });
});
