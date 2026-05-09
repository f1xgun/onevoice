'use client';

// Linen rebuild.
// The right-rail companion: an AI-rendered understanding of the business
// based on what the owner has filled in, plus quiet tips. Read-only —
// the owner verifies, then keeps editing the form.

import { useTranslations } from 'next-intl';
import { MonoLabel } from '@/components/ui/mono-label';
import type { Business } from '@/types/business';
import { toneLabel, type ToneId } from '@/lib/tones';

const CATEGORY_LABEL: Record<string, string> = {
  cafe: 'кофейня',
  retail: 'магазин',
  service: 'студия услуг',
  beauty: 'студия красоты',
  education: 'учебный центр',
  other: 'локальный бизнес',
};

// Truncation thresholds for the AI summary preview. We only truncate
// when the description exceeds DESCRIPTION_PREVIEW_MAX_LEN, and we slice
// at DESCRIPTION_PREVIEW_TRIM_LEN so the appended ellipsis fits within
// the original cap.
const DESCRIPTION_PREVIEW_MAX_LEN = 120;
const DESCRIPTION_PREVIEW_TRIM_LEN = 117;

function buildSummary(business: Partial<Business> | undefined, tones: ToneId[]): string {
  if (!business) {
    return 'Заполните основное и OneVoice опишет ваш бизнес здесь — так вы увидите, как это прозвучит для клиентов.';
  }
  const name = business.name?.trim() || 'Ваш бизнес';
  const kind = (business.category && CATEGORY_LABEL[business.category]) || 'локальный бизнес';
  const where = business.address?.split(',')[0]?.trim();
  const description = business.description?.trim();

  const parts: string[] = [];
  parts.push(
    `OneVoice описывает вас как ${kind}${name ? ` «${name}»` : ''}${where ? ` — ${where}` : ''}.`
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
    parts.push(`Тон — ${list}.`);
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
