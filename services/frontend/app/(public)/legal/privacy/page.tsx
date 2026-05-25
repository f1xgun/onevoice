// Phase 22-02 — Surface A: Privacy Policy page (/legal/privacy).
//
// Async Server Component. Reads content/legal/privacy.{locale}.md at
// render time via loadLegalDoc(), renders the body markdown via
// react-markdown, mounts LegalPageHeader (title + EffectiveTag) and
// DataControllerBlock (Art. 14 §3) below the body. Public route —
// reachable without auth (legal documents must always be accessible
// per the 152-ФЗ "accessible policy" requirement).

import ReactMarkdown from 'react-markdown';
import { getLocale } from 'next-intl/server';
import { LegalPageHeader } from '@/components/legal/LegalPageHeader';
import { DataControllerBlock } from '@/components/legal/DataControllerBlock';
import { loadLegalDoc } from '@/lib/legal/loader';
import type { Locale } from '@/lib/i18n/locales';

export default async function PrivacyPage() {
  const locale = (await getLocale()) as Locale;
  const doc = await loadLegalDoc('privacy', locale);
  return (
    <>
      <LegalPageHeader title={doc.title} version={doc.version} effectiveFrom={doc.effectiveFrom} />
      <div className="space-y-4 text-[15px] leading-[1.55] text-[var(--ov-ink)] [&_a]:text-[var(--ov-accent)] [&_a]:hover:underline [&_h2]:mt-8 [&_h2]:text-[24px] [&_h2]:font-medium [&_h2]:leading-[1.2] [&_h2]:tracking-[-0.015em] [&_h3]:mt-6 [&_h3]:text-[18px] [&_h3]:font-medium [&_li]:mt-1 [&_p]:mt-3 [&_ul]:mt-3 [&_ul]:list-disc [&_ul]:pl-6">
        <ReactMarkdown>{doc.bodyMarkdown}</ReactMarkdown>
      </div>
      {doc.showsController && <DataControllerBlock />}
    </>
  );
}
