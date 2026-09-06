const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ replace }) }));
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

// Mocks must be declared before the SUT import.

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: vi.fn(),
}));

vi.mock('@/lib/hooks/useBusinessList', () => ({
  useBusinessList: vi.fn(),
  BUSINESS_LIST_QUERY_KEY: ['businesses'],
}));

import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { BusinessSwitcher } from '../BusinessSwitcher';

const mockedStore = vi.mocked(useBusinessStore);
const mockedList = vi.mocked(useBusinessList);

function wrap(node: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

interface StoreShape {
  activeBusinessId: string | null;
  setActive: (id: string | null) => void;
  clear?: () => void;
}

function arrange(opts: {
  active: string | null;
  businesses: Array<{ id: string; name: string; role: string }>;
  isLoading?: boolean;
  setActive?: (id: string | null) => void;
}) {
  const setActive = opts.setActive ?? vi.fn();
  mockedStore.mockImplementation((selector: unknown) =>
    (selector as (s: StoreShape) => unknown)({
      activeBusinessId: opts.active,
      setActive,
    })
  );
  mockedList.mockReturnValue({
    data: opts.businesses.map((b) => ({
      id: b.id,
      name: b.name,
      role: { id: 'r-' + b.role, name: b.role },
      status: 'active' as const,
      joined_at: '2026-05-10T00:00:00Z',
    })),
    isLoading: opts.isLoading ?? false,
  } as unknown as ReturnType<typeof useBusinessList>);
  return { setActive };
}

beforeEach(() => {
  mockedStore.mockReset();
  mockedList.mockReset();
});

describe('BusinessSwitcher', () => {
  it('renders the active business initials in the trigger', () => {
    arrange({ active: 'b1', businesses: [{ id: 'b1', name: 'Acme', role: 'admin' }] });
    render(wrap(<BusinessSwitcher />));
    const trigger = screen.getByRole('button', { name: /Acme/ });
    expect(trigger).toBeInTheDocument();
    expect(within(trigger).getByText('AC')).toBeInTheDocument();
  });

  it('opens popover on click and lists all businesses', () => {
    arrange({
      active: 'b1',
      businesses: [
        { id: 'b1', name: 'Acme', role: 'owner' },
        { id: 'b2', name: 'Bravo', role: 'editor' },
      ],
    });
    render(wrap(<BusinessSwitcher />));
    fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
    const menu = screen.getByRole('menu');
    expect(within(menu).getAllByRole('menuitemradio')).toHaveLength(2);
    expect(within(menu).getByText('Acme')).toBeInTheDocument();
    expect(within(menu).getByText('Bravo')).toBeInTheDocument();
  });

  it('calls setActive(id) on row selection', () => {
    const setActive = vi.fn();
    arrange({
      active: 'b1',
      businesses: [
        { id: 'b1', name: 'Acme', role: 'owner' },
        { id: 'b2', name: 'Bravo', role: 'editor' },
      ],
      setActive,
    });
    render(wrap(<BusinessSwitcher />));
    fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
    const bravoRow = screen.getByRole('menuitemradio', { name: /Bravo/ });
    fireEvent.click(bravoRow);
    expect(setActive).toHaveBeenCalledWith('b2');
  });

  it('marks the active row with aria-checked=true and other rows with false', () => {
    arrange({
      active: 'b1',
      businesses: [
        { id: 'b1', name: 'Acme', role: 'owner' },
        { id: 'b2', name: 'Bravo', role: 'editor' },
      ],
    });
    render(wrap(<BusinessSwitcher />));
    fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
    const acme = screen.getByRole('menuitemradio', { name: /Acme/ });
    const bravo = screen.getByRole('menuitemradio', { name: /Bravo/ });
    expect(acme).toHaveAttribute('aria-checked', 'true');
    expect(bravo).toHaveAttribute('aria-checked', 'false');
  });

  it('renders a Plus trigger (empty aria-label) when there are 0 businesses', () => {
    arrange({ active: null, businesses: [] });
    render(wrap(<BusinessSwitcher />));
    const trigger = screen.getByRole('button', { name: /нет организаций/i });
    expect(trigger).toBeInTheDocument();
  });

  it('opens popover and shows the Create CTA when there is 1 business (Open Question #1 resolution)', () => {
    arrange({ active: 'b1', businesses: [{ id: 'b1', name: 'Acme', role: 'owner' }] });
    render(wrap(<BusinessSwitcher />));
    fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
    expect(screen.getByRole('menuitemradio', { name: /Acme/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Создать организацию/ })).toHaveAttribute(
      'href',
      '/business/new'
    );
  });

  it('contains an aria-live polite region for selection announcements', () => {
    arrange({ active: 'b1', businesses: [{ id: 'b1', name: 'Acme', role: 'owner' }] });
    render(wrap(<BusinessSwitcher />));
    fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
    const menu = screen.getByRole('menu');
    expect(menu.querySelector('[aria-live="polite"]')).not.toBeNull();
  });

  it('trigger aria-label uses the team.switcher.triggerAria template', () => {
    arrange({ active: 'b1', businesses: [{ id: 'b1', name: 'Acme', role: 'owner' }] });
    render(wrap(<BusinessSwitcher />));
    expect(
      screen.getByRole('button', { name: 'Acme, переключить организацию' })
    ).toBeInTheDocument();
  });
});

it('cancels previous tenant queries and opens the chat list on switch', () => {
  const { setActive } = arrange({
    active: 'b1',
    businesses: [
      { id: 'b1', name: 'Acme', role: 'owner' },
      { id: 'b2', name: 'Bravo', role: 'owner' },
    ],
  });
  const client = new QueryClient();
  const cancel = vi.spyOn(client, 'cancelQueries');
  render(
    <QueryClientProvider client={client}>
      <BusinessSwitcher />
    </QueryClientProvider>
  );
  fireEvent.click(screen.getByRole('button', { name: /Acme/ }));
  fireEvent.click(screen.getByText('Bravo'));
  expect(cancel).toHaveBeenCalledOnce();
  expect(replace).toHaveBeenCalledWith('/chat');
  expect(setActive).toHaveBeenCalledWith('b2');
});
