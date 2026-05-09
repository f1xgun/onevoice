'use client';

// Linen rebuild — Phase 4.8.
// The right-rail companion: an AI-rendered understanding of the business
// based on what the owner has filled in, plus quiet tips. Read-only —
// the owner verifies, then keeps editing the form.

import { useTranslations } from 'next-intl';
import { MonoLabel } from '@/components/ui/mono-label';
import type { Business } from '@/types/business';
import { getTranslator } from '@/lib/i18n/translator';
import { toneLabel, type ToneId } from '@/lib/tones';

// Module-level translators for `buildSummary` (a plain helper, not a
// hook). Two namespaces:
//   business.categoriesShort.* — short kind labels for the summary
//     ("кофейня", "магазин"); distinct from business.categories.* which
//     drives the form select.
//   business.aiSummaryRail.*   — the assembled-summary copy.
const tCategoriesShort = getTranslator('business.categoriesShort');
const tRail = getTranslator('business.aiSummaryRail');

// Truncation thresholds for the AI summary preview. We only truncate
// when the description exceeds DESCRIPTION_PREVIEW_MAX_LEN, and we slice
// at DESCRIPTION_PREVIEW_TRIM_LEN so the appended ellipsis fits within
// the original cap.
const DESCRIPTION_PREVIEW_MAX_LEN = 120;
const DESCRIPTION_PREVIEW_TRIM_LEN = 117;

const KNOWN_CATEGORIES = new Set(['cafe', 'retail', 'service', 'beauty', 'education', 'other']);

function buildSummary(business: Partial<Business> | undefined, tones: ToneId[]): string {
  if (!business) {
    return tRail('placeholder');
  }
  const name = business.name?.trim() || tRail('nameFallback');
  const kind =
    business.category && KNOWN_CATEGORIES.has(business.category)
      ? tCategoriesShort(business.category as Parameters<typeof tCategoriesShort>[0])
      : tCategoriesShort('other');
  const where = business.address?.split(',')[0]?.trim();
  const description = business.description?.trim();

  const parts: string[] = [];
  // ICU select keeps the optional name/where fragments out of TS — the
  // `hasName`/`hasWhere` boolean params drive whether the brace block is
  // emitted, so the message stays a single key without four variants.
  parts.push(
    tRail('describes', {
      kind,
      hasName: name ? 'true' : 'false',
      name,
      hasWhere: where ? 'true' : 'false',
      where: where ?? '',
    })
  );
  if (description) {
    const short =
      description.length > DESCRIPTION_PREVIEW_MAX_LEN
        ? `${description.slice(0, DESCRIPTION_PREVIEW_TRIM_LEN).trim()}…`
        : description;
    parts.push(short);
  }
  if (tones.length > 0) {
    const list = tones.map((id) => toneLabel(id).toLowerCase()).join(', ');
    parts.push(tRail('toneLine', { list }));
  }
  return parts.join(' ');
}

export interface AISummaryRailProps {
  business?: Partial<Business>;
  tones: ToneId[];
}

export function AISummaryRail({ business, tones }: AISummaryRailProps) {
  const t = useTranslations('business.aiSummary');
  const summary = buildSummary(business, tones);

  return (
    <aside className="flex flex-col gap-3 lg:sticky lg:top-8 lg:self-start">
      {/* AI understanding */}
      <section className="flex flex-col gap-3 rounded-lg border border-line bg-paper-sunken p-5">
        <MonoLabel>{t('sample')}</MonoLabel>
        <p className="text-sm leading-relaxed text-ink">{summary}</p>
        <p className="text-xs leading-relaxed text-ink-mid">{t('sampleBody')}</p>
      </section>

      {/* Tips */}
      <section className="flex flex-col gap-3 rounded-lg border border-line bg-paper-raised p-5">
        <MonoLabel>{t('tips')}</MonoLabel>
        <ul className="flex flex-col gap-2 text-[13px] leading-relaxed text-ink-mid">
          <li>{t('tip1')}</li>
          <li>{t('tip2')}</li>
          <li>{t('tip3')}</li>
        </ul>
      </section>
    </aside>
  );
}
