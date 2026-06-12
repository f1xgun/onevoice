import { describe, expect, it } from 'vitest';
import { isPlaceholder, validateINN, validateLegalEntityEnv } from '../check-legal-entity.mjs';

describe('isPlaceholder', () => {
  it('treats empty / whitespace / templates / sentinels as placeholders', () => {
    expect(isPlaceholder('')).toBe(true);
    expect(isPlaceholder('   ')).toBe(true);
    expect(isPlaceholder('[Юридическое лицо — будет обновлено]')).toBe(true);
    expect(isPlaceholder('—')).toBe(true);
    expect(isPlaceholder('-')).toBe(true);
    expect(isPlaceholder('TBD')).toBe(true);
    expect(isPlaceholder('будет позже')).toBe(true);
  });

  it('accepts real values', () => {
    expect(isPlaceholder('ООО ВанВойс')).toBe(false);
    expect(isPlaceholder('7707083893')).toBe(false);
  });
});

describe('validateINN (mirrors backend FNS checksum)', () => {
  it('accepts a valid 10-digit INN', () => {
    expect(validateINN('7707083893')).toBe('');
  });

  it('accepts a valid 12-digit INN', () => {
    expect(validateINN('500100732259')).toBe('');
  });

  it('rejects wrong length / non-digits', () => {
    expect(validateINN('123')).toBe('INN must be 10 or 12 digits');
    expect(validateINN('77070838ab')).toBe('INN must be 10 or 12 digits');
  });

  it('rejects a checksum mismatch', () => {
    expect(validateINN('7707083894')).toBe('INN checksum mismatch');
  });
});

describe('validateLegalEntityEnv', () => {
  const valid = {
    NEXT_PUBLIC_LEGAL_ENTITY_NAME: 'ООО ВанВойс',
    NEXT_PUBLIC_LEGAL_INN: '7707083893',
    NEXT_PUBLIC_LEGAL_ADDRESS: '109012, г. Москва, ул. Ильинка, д. 9',
    NEXT_PUBLIC_LEGAL_EMAIL_PDN: 'pdn@onevoice.ru',
  };

  it('passes a fully-populated valid entity', () => {
    expect(validateLegalEntityEnv(valid)).toEqual([]);
  });

  it('reports every problem when all are unset (the placeholder-ship risk)', () => {
    const problems = validateLegalEntityEnv({});
    expect(problems).toHaveLength(4);
    expect(problems.join('\n')).toContain('NEXT_PUBLIC_LEGAL_ENTITY_NAME');
    expect(problems.join('\n')).toContain('NEXT_PUBLIC_LEGAL_INN');
    expect(problems.join('\n')).toContain('NEXT_PUBLIC_LEGAL_ADDRESS');
    expect(problems.join('\n')).toContain('NEXT_PUBLIC_LEGAL_EMAIL_PDN');
  });

  it('rejects a Latin-only name (>=4 Cyrillic rule)', () => {
    const problems = validateLegalEntityEnv({
      ...valid,
      NEXT_PUBLIC_LEGAL_ENTITY_NAME: 'OneVoice LLC',
    });
    expect(problems).toContain(
      'NEXT_PUBLIC_LEGAL_ENTITY_NAME must contain >=4 Cyrillic characters'
    );
  });

  it('rejects a short address', () => {
    const problems = validateLegalEntityEnv({ ...valid, NEXT_PUBLIC_LEGAL_ADDRESS: 'Москва' });
    expect(problems).toContain('NEXT_PUBLIC_LEGAL_ADDRESS must be at least 20 characters');
  });

  it('rejects a malformed email', () => {
    const problems = validateLegalEntityEnv({
      ...valid,
      NEXT_PUBLIC_LEGAL_EMAIL_PDN: 'not-an-email',
    });
    expect(problems).toContain('NEXT_PUBLIC_LEGAL_EMAIL_PDN is not a valid email address');
  });
});
