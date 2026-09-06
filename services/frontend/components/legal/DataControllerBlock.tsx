'use client';

import { useTranslations } from 'next-intl';
import { isPlaceholder, loadLegalEntity } from '@/lib/legal/entity';

export function DataControllerBlock() {
  const t = useTranslations('legal.shared');
  const entity = loadLegalEntity();

  if (isPlaceholder(entity)) return null;

  return (
    <section
      aria-label={t('controllerHeading')}
      className="my-8 rounded-xl border border-[var(--ov-line)] bg-[var(--ov-paper-raised)] p-6"
    >
      <h2 className="mb-4 text-[18px] font-medium text-[var(--ov-ink)]">
        {t('controllerHeading')}
      </h2>
      <dl className="grid gap-3 text-[15px] text-[var(--ov-ink-mid)] [overflow-wrap:anywhere] md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] md:gap-x-6 md:gap-y-2 [&>dd]:min-w-0 [&>dt]:min-w-0">
        <dt className="font-medium text-[var(--ov-ink)]">{t('controllerOperatorLabel')}</dt>
        <dd>{entity.name}</dd>
        <dt className="font-medium text-[var(--ov-ink)]">{t('controllerInnLabel')}</dt>
        <dd>{entity.inn || '—'}</dd>
        <dt className="font-medium text-[var(--ov-ink)]">{t('controllerAddressLabel')}</dt>
        <dd>{entity.address || '—'}</dd>
        <dt className="font-medium text-[var(--ov-ink)]">{t('controllerEmailLabel')}</dt>
        <dd>
          {entity.emailPdn && entity.emailPdn !== '—' ? (
            <a
              href={`mailto:${entity.emailPdn}`}
              className="text-[var(--ov-accent)] hover:underline"
            >
              {entity.emailPdn}
            </a>
          ) : (
            '—'
          )}
        </dd>
      </dl>
    </section>
  );
}
