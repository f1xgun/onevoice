import { expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DataControllerBlock } from '../DataControllerBlock';

const { entity } = vi.hoisted(() => ({
  entity: {
    name: 'Организация'.repeat(30),
    inn: '1234567890',
    address: 'ДлинныйАдрес'.repeat(40),
    emailPdn: `${'long'.repeat(40)}@example.test`,
  },
}));
vi.mock('@/lib/legal/entity', () => ({
  loadLegalEntity: () => entity,
  isPlaceholder: () => false,
}));

it('renders complete legal values with shrinking tracks and arbitrary word wrapping', () => {
  render(<DataControllerBlock />);
  const name = screen.getByText(entity.name);
  const list = name.closest('dl');
  expect(list).toHaveClass(
    'md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]',
    '[overflow-wrap:anywhere]',
    '[&>dd]:min-w-0'
  );
  expect(screen.getByText(entity.address)).toBeVisible();
  expect(screen.getByRole('link', { name: entity.emailPdn })).toHaveAttribute(
    'href',
    `mailto:${entity.emailPdn}`
  );
});
