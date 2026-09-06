import { expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AISummaryRail } from '../AISummaryRail';

it('keeps an unbroken organization name within the shrinking summary column', () => {
  const name = 'Организация'.repeat(40);
  const { container } = render(<AISummaryRail business={{ name }} tones={[]} />);
  expect(screen.getByText(new RegExp(name))).toBeVisible();
  expect(container.querySelector('aside')).toHaveClass(
    'min-w-0',
    '[overflow-wrap:anywhere]',
    'xl:sticky'
  );
});
