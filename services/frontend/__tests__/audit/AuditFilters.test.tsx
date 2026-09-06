import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// Stub the two filter sub-widgets so the test isolates the AuditFilters
// chip-toggle + action-select logic.
vi.mock('@/app/(app)/settings/audit/_components/ActorAutocomplete', () => ({
  ActorAutocomplete: () => <div data-testid="actor-stub" />,
}));
vi.mock('@/app/(app)/settings/audit/_components/DateRangePicker', () => ({
  DateRangePicker: () => <div data-testid="daterange-stub" />,
}));

import { AuditFilters } from '@/app/(app)/settings/audit/_components/AuditFilters';

describe('AuditFilters', () => {
  it('renders all six category chips with the default selected', () => {
    const onChange = vi.fn();
    render(<AuditFilters value={{ category: 'all' }} onChange={onChange} businessID="b" />);
    expect(screen.getByTestId('cat-chip-all')).toHaveAttribute('aria-selected', 'true');
    for (const c of ['rbac', 'auth', 'integration', 'business', 'project']) {
      expect(screen.getByTestId(`cat-chip-${c}`)).toBeInTheDocument();
    }
  });

  it('toggles category chip and clears the action selection', () => {
    const onChange = vi.fn();
    render(
      <AuditFilters
        value={{ category: 'all', action: 'rbac.role_granted' }}
        onChange={onChange}
        businessID="b"
      />
    );
    fireEvent.click(screen.getByTestId('cat-chip-auth'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ category: 'auth', action: undefined })
    );
  });

  it('scopes the action <select> options to the selected category', () => {
    render(<AuditFilters value={{ category: 'auth' }} onChange={() => {}} businessID="b" />);
    const select = screen.getByTestId('action-select') as HTMLSelectElement;
    const opts = Array.from(select.options).map((o) => o.value);
    expect(opts).toContain('');
    expect(opts).toContain('auth.login_success');
    expect(opts).toContain('auth.login_failed');
    expect(opts).toContain('auth.logout');
    expect(opts).not.toContain('rbac.role_granted');
  });

  it('emits a new action filter when the user selects one', () => {
    const onChange = vi.fn();
    render(<AuditFilters value={{ category: 'rbac' }} onChange={onChange} businessID="b" />);
    fireEvent.change(screen.getByTestId('action-select'), {
      target: { value: 'rbac.role_granted' },
    });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ action: 'rbac.role_granted' }));
  });
});

it('constrains the native action selector independently of option length', () => {
  render(<AuditFilters value={{ category: 'all' }} onChange={vi.fn()} businessID="b" />);
  const select = screen.getByTestId('action-select');
  expect(select).toHaveClass('w-full', 'min-w-0', 'max-w-full');
  expect(select.parentElement).toHaveClass('w-full', 'min-w-0', 'sm:w-64');
});
