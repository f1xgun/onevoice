// Voice/tone vocabulary for the business profile. Stored as stable enum
// ids (e.g. "warm") in business.settings.voiceTone — labels are rendered
// via this table so the DB stays locale-agnostic. Adding an `en` field is
// the only change needed for English UI.

export const TONE_OPTIONS = [
  { id: 'warm', label: { ru: 'Тёплый' } },
  { id: 'calm', label: { ru: 'Спокойный' } },
  { id: 'friendly', label: { ru: 'Дружеский' } },
  { id: 'professional', label: { ru: 'Профессиональный' } },
  { id: 'playful', label: { ru: 'Игривый' } },
  { id: 'businesslike', label: { ru: 'Деловой' } },
] as const;

export type ToneId = (typeof TONE_OPTIONS)[number]['id'];

const VALID_IDS = new Set<string>(TONE_OPTIONS.map((o) => o.id));
const RU_LABEL_TO_ID: Record<string, ToneId> = TONE_OPTIONS.reduce(
  (acc, o) => {
    acc[o.label.ru.toLowerCase()] = o.id;
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

export function toneLabel(id: ToneId, locale: 'ru' = 'ru'): string {
  const opt = TONE_OPTIONS.find((o) => o.id === id);
  return opt ? opt.label[locale] : id;
}
