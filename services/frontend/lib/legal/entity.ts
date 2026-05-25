// Phase 22-02 — Reads NEXT_PUBLIC_LEGAL_* environment variables for the
// 152-ФЗ Art. 14 §3 data controller block (Surfaces A, C, contact) and
// the Footer copyright/contact line (Surface G).
//
// When any are missing or still placeholders, isPlaceholder() returns
// true and consumers render fallback copy + console.warn (D-22). Per
// D-21 the .env.example ships with these placeholders + comments
// instructing the operator to fill them in before staging deploy; the
// Phase 22-03 launch checklist asserts non-placeholder.

export interface LegalEntity {
  name: string;
  inn: string;
  address: string;
  emailPdn: string;
}

// PLACEHOLDER_NAME is the literal D-22 mandates when LEGAL_ENTITY_NAME
// is unset. Render it verbatim so a deploy-time check can grep for the
// string and flag the operator handoff as incomplete.
const PLACEHOLDER_NAME = '[Юридическое лицо — будет обновлено]';
const PLACEHOLDER_EMAIL = '—';

export function loadLegalEntity(): LegalEntity {
  return {
    name: process.env.NEXT_PUBLIC_LEGAL_ENTITY_NAME || PLACEHOLDER_NAME,
    inn: process.env.NEXT_PUBLIC_LEGAL_INN || '',
    address: process.env.NEXT_PUBLIC_LEGAL_ADDRESS || '',
    emailPdn: process.env.NEXT_PUBLIC_LEGAL_EMAIL_PDN || PLACEHOLDER_EMAIL,
  };
}

export function isPlaceholder(e: LegalEntity): boolean {
  return (
    e.name === PLACEHOLDER_NAME ||
    e.name === '' ||
    e.inn === '' ||
    e.address === '' ||
    e.emailPdn === PLACEHOLDER_EMAIL ||
    e.emailPdn === ''
  );
}
