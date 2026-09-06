import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DataControllerBlock } from '../DataControllerBlock';
import { Footer } from '@/components/layout/Footer';
import { isPlaceholder, loadLegalEntity } from '@/lib/legal/entity';

const valid = {
  NEXT_PUBLIC_LEGAL_ENTITY_NAME: 'ООО Пример',
  NEXT_PUBLIC_LEGAL_INN: '7707083893',
  NEXT_PUBLIC_LEGAL_ADDRESS: 'Москва, ул. Примерная, 1',
  NEXT_PUBLIC_LEGAL_EMAIL_PDN: 'privacy@example.org',
};

afterEach(() => vi.unstubAllEnvs());

describe('legal entity visibility', () => {
  it.each([
    {},
    { NEXT_PUBLIC_LEGAL_ENTITY_NAME: 'ООО Пример' },
    { ...valid, NEXT_PUBLIC_LEGAL_ADDRESS: '   ' },
    { ...valid, NEXT_PUBLIC_LEGAL_INN: 'TBD' },
    ...Object.keys(valid).map((key) => ({ ...valid, [key]: 'N/A' })),
  ])('hides the entire incomplete entity while retaining document links', (config) => {
    for (const key of Object.keys(valid)) vi.stubEnv(key, '');
    for (const [key, value] of Object.entries(config)) vi.stubEnv(key, value);
    expect(isPlaceholder(loadLegalEntity())).toBe(true);
    const { container } = render(
      <>
        <DataControllerBlock />
        <Footer />
      </>
    );
    expect(container.querySelector('section')).toBeNull();
    expect(container.querySelector('a[href^="mailto:"]')).toBeNull();
    expect(screen.queryByText(/ООО Пример|будет обновлено|©|—/)).not.toBeInTheDocument();
    for (const path of ['privacy', 'terms', 'consent']) {
      expect(container.querySelector(`a[href="/legal/${path}"]`)).not.toBeNull();
    }
  });

  it('renders complete controller details and footer contact', () => {
    for (const [key, value] of Object.entries(valid)) vi.stubEnv(key, value);
    expect(isPlaceholder(loadLegalEntity())).toBe(false);
    const { container } = render(
      <>
        <DataControllerBlock />
        <Footer />
      </>
    );
    expect(screen.getByText(valid.NEXT_PUBLIC_LEGAL_INN)).toBeInTheDocument();
    expect(screen.getByText(valid.NEXT_PUBLIC_LEGAL_ADDRESS)).toBeInTheDocument();
    expect(container.querySelectorAll('a[href="mailto:privacy@example.org"]')).toHaveLength(2);
  });
});
