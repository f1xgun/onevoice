export interface LegalEntity {
  name: string;
  inn: string;
  address: string;
  emailPdn: string;
}

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

export function isPlaceholder(entity: LegalEntity): boolean {
  return Object.values(entity).some((value) => {
    const trimmed = value.trim();
    return !trimmed || /^(?:—|-|tbd)$/i.test(trimmed) || /\[|будет/i.test(trimmed);
  });
}
