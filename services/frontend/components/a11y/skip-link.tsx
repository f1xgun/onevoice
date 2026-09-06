'use client';

import { useTranslations } from 'next-intl';

export function SkipLink() {
  const t = useTranslations('nav');
  return (
    <a
      href="#main-content"
      className="sr-only fixed left-0 top-0 m-0 max-w-full [contain:strict] focus-visible:not-sr-only focus-visible:fixed focus-visible:left-4 focus-visible:top-4 focus-visible:z-[100] focus-visible:max-w-[calc(100%-2rem)] focus-visible:whitespace-normal focus-visible:rounded-md focus-visible:bg-paper focus-visible:px-4 focus-visible:py-2 focus-visible:text-sm focus-visible:font-medium focus-visible:text-ink focus-visible:shadow-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-ink focus-visible:[contain:none]"
    >
      {t('skipToContent')}
    </a>
  );
}
