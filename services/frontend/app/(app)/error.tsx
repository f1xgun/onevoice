'use client';

// Route-group error boundary for the dashboard. Any uncaught render error in
// an (app) page/component lands here instead of collapsing the whole shell to
// Next's raw default screen — the user keeps a translated, styled recovery
// panel with a retry that re-renders the failed segment.

import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';

export default function AppError({
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const t = useTranslations('common.appError');
  // Rendered nested inside the (app) layout's <main id="main-content">, so this
  // uses a plain alert region rather than a second <main>/duplicate id.
  return (
    <div
      role="alert"
      className="flex min-h-screen w-full items-center justify-center bg-paper px-6 py-16"
    >
      <div className="flex w-full max-w-[480px] flex-col items-center gap-5 rounded-lg border border-line bg-paper-raised px-8 py-20 text-center shadow-ov-1">
        <MonoLabel className="text-ink-soft">{t('errorLabel')}</MonoLabel>
        <div>
          <h1 className="text-[22px] font-medium leading-tight tracking-[-0.01em] text-ink">
            {t('title')}
          </h1>
          <p className="mt-2 text-sm leading-relaxed text-ink-mid">{t('body')}</p>
        </div>
        <Button variant="primary" size="md" onClick={reset}>
          {t('retry')}
        </Button>
      </div>
    </div>
  );
}
