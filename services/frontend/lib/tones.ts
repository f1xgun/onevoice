// Voice/tone vocabulary for the business profile. Stored as stable enum
// ids (e.g. "warm") in business.settings.voiceTone — labels are rendered
// at the call site via `toneLabel(id)` which reads
// `business.voiceTone.options.<id>` from messages/ru.json, so the DB
// stays locale-agnostic.

import { getTranslator } from '@/lib/i18n/translator';

const tToneOptions = getTranslator('business.voiceTone.options');

export const TONE_IDS = [
  'warm',
  'calm',
  'friendly',
  'professional',
  'playful',
  'businesslike',
] as const;

export type ToneId = (typeof TONE_IDS)[number];

// Display option list for the chip selector. Labels are resolved once at
// module load via the module-level translator; the shape mirrors the
// previous `TONE_OPTIONS` so consumers (VoiceToneSection) can keep
// iterating in display order without restructuring.
export const TONE_OPTIONS: ReadonlyArray<{ id: ToneId; label: string }> = TONE_IDS.map((id) => ({
  id,
  label: tToneOptions(id),
}));

const VALID_IDS = new Set<string>(TONE_IDS);
// Pre-built reverse index for the legacy "stored Russian label" branch in
// `normalizeStoredTones`. Keys are lowercased Russian labels — the stored
// data only ever held Russian, so a single-locale lookup is enough.
const RU_LABEL_TO_ID: Record<string, ToneId> = TONE_OPTIONS.reduce(
  (acc, o) => {
    acc[o.label.toLowerCase()] = o.id;
    return acc;
  },
  {} as Record<string, ToneId>
);

export function isToneId(s: string): s is ToneId {
  return VALID_IDS.has(s);
}

// Backwards-compatible read of stored values. Older records (pre-migration)
// hold Russian display labels like "Деловой"; new records hold ids like
// "businesslike". Map both shapes to the canonical id list, drop unknowns.
export function normalizeStoredTones(raw: unknown): ToneId[] {
  if (!Array.isArray(raw)) return [];
  const out: ToneId[] = [];
  const seen = new Set<ToneId>();
  for (const item of raw) {
    if (typeof item !== 'string') continue;
    if (isToneId(item)) {
      if (!seen.has(item)) {
        out.push(item);
        seen.add(item);
      }
      continue;
    }
    const hit = RU_LABEL_TO_ID[item.toLowerCase()];
    if (hit && !seen.has(hit)) {
      out.push(hit);
      seen.add(hit);
    }
  }
  return out;
}

export function toneLabel(id: ToneId): string {
  return isToneId(id) ? tToneOptions(id) : id;
}
