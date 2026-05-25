// Phase 22-02 — Surface A companion: /legal/contact page.
// 152-ФЗ Art. 14 §2 — operator contact and 15-business-day SLA notice.
// Server Component (uses getTranslations from next-intl/server).

import { getTranslations } from 'next-intl/server';
import { DataControllerBlock } from '@/components/legal/DataControllerBlock';

export default async function ContactPage() {
  const t = await getTranslations('legal.contact');
  return (
    <>
      <header className="mb-12">
        <h1 className="text-[32px] font-medium leading-[1.2] tracking-[-0.015em] text-[var(--ov-ink)]">
          {t('title')}
        </h1>
        <p className="mt-2 text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">{t('subtitle')}</p>
      </header>
      <DataControllerBlock />
      <p className="mt-6 text-[15px] leading-[1.55] text-[var(--ov-ink-mid)]">{t('slaNotice')}</p>
    </>
  );
}
