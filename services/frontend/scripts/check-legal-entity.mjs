// Build-time gate for the 152-ФЗ Art. 14 data-controller identity.
//
// The NEXT_PUBLIC_LEGAL_* values are inlined into the client bundle by
// `next build`, so a production image can silently ship the placeholder
// «[Юридическое лицо — будет обновлено]» if they are unset. The API has a
// matching boot gate (services/api/internal/config/validators.go
// validateLegalProduction); this is its frontend counterpart, run from
// `pnpm build` so the same placeholders can never reach a production bundle.
//
// Rules mirror the backend exactly (placeholder regex, >=4 Cyrillic name,
// ИНН 10/12-digit FNS checksum, >=20-char address, parseable email). The gate
// only enforces when APP_ENV=production; local and CI builds (APP_ENV unset)
// skip it so they don't need real legal data.

import { fileURLToPath } from 'node:url';

// Mirrors placeholderRe in services/api/internal/config/validators.go: bracket
// templates, em-dash / hyphen sentinels, TBD / N/A, and Russian «будет …».
const PLACEHOLDER_RE = /^(\[.*\]|—|-|tbd|n\/a|будет .*)$/i;
const INN_DIGITS_RE = /^(\d{10}|\d{12})$/;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const MIN_CYRILLIC_CHARS = 4;
const MIN_ADDRESS_RUNES = 20;

export function isPlaceholder(value) {
  const s = (value ?? '').trim();
  return s === '' || PLACEHOLDER_RE.test(s);
}

function cyrillicCount(s) {
  return [...s].filter((ch) => /\p{Script=Cyrillic}/u.test(ch)).length;
}

// validateINN mirrors validateINN in the backend: digit shape + the FNS
// control-digit checksum for 10-digit (ООО) and 12-digit (ИП) numbers.
export function validateINN(inn) {
  if (!INN_DIGITS_RE.test(inn)) {
    return 'INN must be 10 or 12 digits';
  }
  const d = [...inn].map((c) => c.charCodeAt(0) - '0'.charCodeAt(0));
  if (d.length === 10) {
    const w = [2, 4, 10, 3, 5, 9, 4, 6, 8, 0];
    let sum = 0;
    for (let i = 0; i < 9; i++) sum += d[i] * w[i];
    return (sum % 11) % 10 === d[9] ? '' : 'INN checksum mismatch';
  }
  const w1 = [7, 2, 4, 10, 3, 5, 9, 4, 6, 8, 0];
  const w2 = [3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8, 0];
  let sum1 = 0;
  let sum2 = 0;
  for (let i = 0; i < 11; i++) sum1 += d[i] * w1[i];
  for (let i = 0; i < 12; i++) sum2 += d[i] * w2[i];
  const ok = (sum1 % 11) % 10 === d[10] && (sum2 % 11) % 10 === d[11];
  return ok ? '' : 'INN checksum mismatch';
}

// validateLegalEntityEnv returns the list of problems (empty when valid). env is
// any object exposing the four NEXT_PUBLIC_LEGAL_* keys (e.g. process.env).
export function validateLegalEntityEnv(env) {
  const name = (env.NEXT_PUBLIC_LEGAL_ENTITY_NAME ?? '').trim();
  const inn = (env.NEXT_PUBLIC_LEGAL_INN ?? '').trim();
  const address = (env.NEXT_PUBLIC_LEGAL_ADDRESS ?? '').trim();
  const emailPdn = (env.NEXT_PUBLIC_LEGAL_EMAIL_PDN ?? '').trim();

  const problems = [];

  if (isPlaceholder(name)) {
    problems.push('NEXT_PUBLIC_LEGAL_ENTITY_NAME is empty or placeholder');
  } else if (cyrillicCount(name) < MIN_CYRILLIC_CHARS) {
    problems.push('NEXT_PUBLIC_LEGAL_ENTITY_NAME must contain >=4 Cyrillic characters');
  }

  if (isPlaceholder(inn)) {
    problems.push('NEXT_PUBLIC_LEGAL_INN is empty or placeholder');
  } else {
    const innErr = validateINN(inn);
    if (innErr) problems.push('NEXT_PUBLIC_LEGAL_INN: ' + innErr);
  }

  if (isPlaceholder(address)) {
    problems.push('NEXT_PUBLIC_LEGAL_ADDRESS is empty or placeholder');
  } else if ([...address].length < MIN_ADDRESS_RUNES) {
    problems.push('NEXT_PUBLIC_LEGAL_ADDRESS must be at least 20 characters');
  }

  if (isPlaceholder(emailPdn)) {
    problems.push('NEXT_PUBLIC_LEGAL_EMAIL_PDN is empty or placeholder');
  } else if (!EMAIL_RE.test(emailPdn)) {
    problems.push('NEXT_PUBLIC_LEGAL_EMAIL_PDN is not a valid email address');
  }

  return problems;
}

function main() {
  const appEnv = (process.env.APP_ENV ?? '').trim().toLowerCase();
  if (appEnv !== 'production') {
    console.log(
      `[legal-gate] APP_ENV=${appEnv || '(unset)'} — skipping data-controller check (enforced only when APP_ENV=production).`
    );
    return;
  }

  const problems = validateLegalEntityEnv(process.env);
  if (problems.length > 0) {
    console.error(
      '[legal-gate] Refusing to build: the 152-ФЗ Art. 14 data-controller identity is incomplete.\n' +
        problems.map((p) => `  - ${p}`).join('\n') +
        '\nSet the NEXT_PUBLIC_LEGAL_* build args to real values (see .env.example §13).'
    );
    process.exit(1);
  }
  console.log('[legal-gate] data-controller identity present and valid.');
}

const invokedDirectly = process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1];
if (invokedDirectly) {
  main();
}
