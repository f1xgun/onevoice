// Voice/tone vocabulary for the business profile. Stored as stable enum
// ids (e.g. "warm") in business.settings.voiceTone — labels are rendered
// at the call site via the request-scoped translator, so the DB stays
// locale-agnostic.

// Translator shape we depend on. Compatible with both
// `useTranslations('business.voiceTone.options')` (React) and
// `getServerTranslator(...)` (async server) outputs. Declared structurally
// so callers don't have to import next-intl just to use this module.
type ToneTranslator = (key: ToneId) => string;

export const TONE_IDS = [
  'warm',
  'calm',
  'friendly',
  'professional',
  'playful',
  'businesslike',
] as const;

export type ToneId = (typeof TONE_IDS)[number];

export interface ToneOption {
  id: ToneId;
  label: string;
}

// Factory variant of the old `TONE_OPTIONS` constant. Call from inside a
// React component or memo via `useMemo(() => createToneOptions(t), [t])`
// where `t = useTranslations('business.voiceTone.options')`. The factory
// pattern (B1) avoids freezing the labels at module load.
export function createToneOptions(t: ToneTranslator): ReadonlyArray<ToneOption> {
  return TONE_IDS.map((id) => ({ id, label: t(id) }));
}

// Same idea, but for a single id — used inline by `<VoiceToneSection>`'s
// chip rendering and by `<AISummaryRail>`'s tone list. Returns the
// localized label or the id itself if the id is unknown (defensive).
export function createToneLabel(t: ToneTranslator): (id: ToneId | string) => string {
  return (id) => (isToneId(id) ? t(id) : id);
}

const VALID_IDS = new Set<string>(TONE_IDS);

// Legacy RU-label → canonical-id index. The stored data only ever held
// Russian display labels, so a single-locale reverse lookup is enough to
// migrate old records. Keys are lowercased for the case-insensitive
// match in `normalizeStoredTones`.
//
// HARDCODED on purpose: this is migration data, not user-facing copy.
// The values mirror `business.voiceTone.options.*` in messages/ru.json
// at the time of the migration. If those strings ever change in ru.json,
// update this table in lockstep so legacy reads keep mapping.
const RU_LABEL_TO_ID: Record<string, ToneId> = {
  тёплый: 'warm',
  теплый: 'warm', // alt diacritic
  спокойный: 'calm',
  дружелюбный: 'friendly',
  профессиональный: 'professional',
  игривый: 'playful',
  деловой: 'businesslike',
};

export function isToneId(s: string): s is ToneId {
  return VALID_IDS.has(s);
}

// Backwards-compatible read of stored values. Older records (pre-migration)
// hold Russian display labels like "Деловой"; new records hold ids like
// "businesslike". Map both shapes to the canonical id list, drop unknowns.
// Label-free utility — no translator argument needed.
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
