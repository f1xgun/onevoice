// Surface A/B/C shared «Действует с {date} · версия {v}» tag.
//
// Per UI-SPEC §A Verbatim copy table: legal.shared.effectiveTag =
// «Действует с {effectiveFrom} · версия {version}». Note the middle-dot
// «·» (U+00B7) — FLAG-A acceptable per the master copy table.

'use client';

import { useFormatter, useTranslations } from 'next-intl';

interface EffectiveTagProps {
  effectiveFrom: string;
  version: string;
}

export function EffectiveTag({ effectiveFrom, version }: EffectiveTagProps) {
  const t = useTranslations('legal.shared');
  const format = useFormatter();
  let dateLabel = effectiveFrom;
  if (effectiveFrom) {
    const parsed = new Date(effectiveFrom);
    if (!Number.isNaN(parsed.getTime())) {
      dateLabel = format.dateTime(parsed, {
        day: 'numeric',
        month: 'long',
        year: 'numeric',
      });
    }
  } else {
    dateLabel = t('versionMissing');
  }

  return (
    <p className="mt-2 text-[13px] leading-[1.4] text-[var(--ov-ink-mid)]">
      {t('effectiveTag', {
        effectiveFrom: dateLabel,
        version: version || t('versionMissing'),
      })}
    </p>
  );
}
