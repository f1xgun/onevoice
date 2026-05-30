// Surface A/C/contact: 152-ФЗ Art. 14 §3 data controller
// block. Renders the legal entity's reqisites (наименование, ИНН,
// юридический адрес, e-mail для запросов по ПДн) read from
// NEXT_PUBLIC_LEGAL_* envs via loadLegalEntity.
//
// Mounted by the page-level Server Components conditionally on
// frontmatter `showsController: true`. Marked 'use client' because
// isPlaceholder emits a console.warn in development which
// requires a browser context.

'use client';

import { useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { isPlaceholder, loadLegalEntity } from '@/lib/legal/entity';

export function DataControllerBlock() {
  const t = useTranslations('legal.shared');
  const entity = loadLegalEntity();

  // Dev-only warning when env vars are still placeholders. Kept
  // in an effect so SSR output is identical to client-rendered output;
  // the warning fires once per page mount in the browser console.
  useEffect(() => {
    if (isPlaceholder(entity)) {
      // eslint-disable-next-line no-console
      console.warn(
        '[legal] entity placeholders are still in use — set NEXT_PUBLIC_LEGAL_* env vars before staging deploy'
      );
    }
  }, [entity]);

  return (
    <section
      aria-label={t('controllerHeading')}
      className="my-8 rounded-xl border border-[var(--ov-line)] bg-[var(--ov-paper-raised)] p-6"
    >
      <h2 className="mb-4 text-[18px] font-medium text-[var(--ov-ink)]">
        {t('controllerHeading')}
      </h2>
      <dl className="grid gap-3 text-[15px] text-[var(--ov-ink-mid)] md:grid-cols-[max-content_1fr] md:gap-x-6 md:gap-y-2">
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
