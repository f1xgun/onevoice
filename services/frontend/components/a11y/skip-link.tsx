'use client';

import { useTranslations } from 'next-intl';

// SkipLink — first focusable element on every page. Anchors to
// `#main-content`, which is the id on the single `<main>` rendered by
// both the authenticated `(app)/layout.tsx` and the public
// `(public)/layout.tsx` shells.
//
// Hidden visually until focused (Tab as the first action on page load
// reveals it). On focus it pops to the top-left with a paper background
// and ink outline — high contrast so keyboard users can find it
// regardless of page background.
//
// Mounted in the root `app/layout.tsx` as the first child of `<body>`
// so it precedes every other tab stop, including the
// `BusinessSwitcher` trigger that opens the desktop NavRail.
export function SkipLink() {
  const t = useTranslations('nav');
  return (
    <a
      href="#main-content"
      className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[100] focus:rounded-md focus:bg-paper focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-ink focus:shadow-lg focus:outline focus:outline-2 focus:outline-ink"
    >
      {t('skipToContent')}
    </a>
  );
}
