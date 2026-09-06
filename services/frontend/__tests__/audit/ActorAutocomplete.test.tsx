import { expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ActorAutocomplete } from '@/app/(app)/settings/audit/_components/ActorAutocomplete';

const { email } = vi.hoisted(() => ({ email: `${'long'.repeat(40)}@example.test` }));
vi.mock('@/lib/hooks/useMembers', () => ({
  useMembers: () => ({ data: [{ user: { id: 'actor', email } }] }),
}));

it.each(['ru', 'en'] as const)(
  'keeps long actor options selectable within the mobile width (%s)',
  (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    const onChange = vi.fn();
    render(<ActorAutocomplete businessID="org" value="actor" onChange={onChange} />);
    const select = screen.getByTestId('actor-select');
    expect(select).toHaveClass('w-full', 'min-w-0', 'max-w-full');
    expect(select.parentElement).toHaveClass('w-full', 'min-w-0', 'max-w-full');
    expect(screen.getByRole('option', { name: email })).toBeInTheDocument();
    fireEvent.change(select, { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith(undefined);
  }
);
