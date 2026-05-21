'use client';

import { useTranslations } from 'next-intl';

export function SkipLink() {
  const t = useTranslations('nav');
  return (
    <a
      href="#main-content"
      // focus-visible: shows ONLY on keyboard focus, so a mouse click on the
      // link doesn't reveal it (axe H3 review). Pairs with tabIndex={-1} on
      // every <main id="main-content"> so activating the link programmatically
      // moves keyboard focus into <main> instead of just scrolling the hash.
      className="sr-only focus-visible:not-sr-only focus-visible:fixed focus-visible:left-4 focus-visible:top-4 focus-visible:z-[100] focus-visible:rounded-md focus-visible:bg-paper focus-visible:px-4 focus-visible:py-2 focus-visible:text-sm focus-visible:font-medium focus-visible:text-ink focus-visible:shadow-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-ink"
    >
      {t('skipToContent')}
    </a>
  );
}
