// Russian plural rule constants. Numbers 11–14 always take the
// genitive-plural form regardless of last digit; 1 takes the
// nominative-singular and 2–4 take the genitive-singular ("paucal")
// form. Exported so the same boundaries are reused everywhere a
// pluralRu-style helper is duplicated (ToolsPageClient, tasks/page,
// chat/ToolCard, YandexBusinessConnectModal, …).
export const RU_PLURAL_TEEN_LOWER = 11;
export const RU_PLURAL_TEEN_UPPER = 14;
export const RU_PLURAL_PAUCAL_UPPER = 4;

/**
 * Russian pluralisation for "чат" per UI-SPEC line 182.
 * n=1, 21, 31, … → "чат"
 * n=2-4, 22-24, … → "чата"
 * n=0, 5-20, 25-30, … → "чатов"
 */
export function chatsPluralRu(n: number): string {
  const abs = Math.abs(n);
  const mod100 = abs % 100;
  const mod10 = abs % 10;
  if (mod100 >= RU_PLURAL_TEEN_LOWER && mod100 <= RU_PLURAL_TEEN_UPPER) return 'чатов';
  if (mod10 === 1) return 'чат';
  if (mod10 >= 2 && mod10 <= RU_PLURAL_PAUCAL_UPPER) return 'чата';
  return 'чатов';
}
