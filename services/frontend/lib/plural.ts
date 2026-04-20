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
  if (mod100 >= 11 && mod100 <= 14) return 'чатов';
  if (mod10 === 1) return 'чат';
  if (mod10 >= 2 && mod10 <= 4) return 'чата';
  return 'чатов';
}
