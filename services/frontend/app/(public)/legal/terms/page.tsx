// Phase 22-02 — Surface B: Terms of Service page (/legal/terms).
// Mirrors privacy/page.tsx but loads slug='terms' and skips
// DataControllerBlock (frontmatter showsController=false per D-08).

import ReactMarkdown from 'react-markdown';
import { getLocale } from 'next-intl/server';
import { LegalPageHeader } from '@/components/legal/LegalPageHeader';
import { loadLegalDoc } from '@/lib/legal/loader';
import type { Locale } from '@/lib/i18n/locales';

export default async function TermsPage() {
  const locale = (await getLocale()) as Locale;
  const doc = await loadLegalDoc('terms', locale);
  return (
    <>
      <LegalPageHeader title={doc.title} version={doc.version} effectiveFrom={doc.effectiveFrom} />
      <div className="space-y-4 text-[15px] leading-[1.55] text-[var(--ov-ink)] [&_a]:text-[var(--ov-accent)] [&_a]:hover:underline [&_h2]:mt-8 [&_h2]:text-[24px] [&_h2]:font-medium [&_h2]:leading-[1.2] [&_h2]:tracking-[-0.015em] [&_h3]:mt-6 [&_h3]:text-[18px] [&_h3]:font-medium [&_li]:mt-1 [&_p]:mt-3 [&_ul]:mt-3 [&_ul]:list-disc [&_ul]:pl-6">
        <ReactMarkdown>{doc.bodyMarkdown}</ReactMarkdown>
      </div>
    </>
  );
}
