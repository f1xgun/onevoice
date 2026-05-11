import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import type { BusinessSummary } from '@/lib/hooks/useBusinessList';
import { BusinessRow } from '../BusinessRow';

function makeBusiness(overrides: Partial<BusinessSummary> = {}): BusinessSummary {
  return {
    id: 'b-1',
    name: 'Acme Co',
    role: { id: 'r-owner', name: 'owner' },
    status: 'active',
    joined_at: '2026-05-10T00:00:00Z',
    ...overrides,
  };
}

describe('BusinessRow', () => {
  it('exposes role="menuitemradio" with aria-checked tied to isActive', () => {
    render(<BusinessRow business={makeBusiness()} isActive={true} onSelect={vi.fn()} />);
    const row = screen.getByRole('menuitemradio');
    expect(row).toHaveAttribute('aria-checked', 'true');
  });

  it('aria-checked is false when not active', () => {
    render(<BusinessRow business={makeBusiness()} isActive={false} onSelect={vi.fn()} />);
    const row = screen.getByRole('menuitemradio');
    expect(row).toHaveAttribute('aria-checked', 'false');
  });

  it('renders the 2px ochre active bar when isActive', () => {
    const { container } = render(
      <BusinessRow business={makeBusiness()} isActive={true} onSelect={vi.fn()} />
    );
    // Mirrors NavRail's absolute -left-N bottom-2 top-2 w-0.5 bg-ochre span.
    const bar = container.querySelector('span.bg-ochre');
    expect(bar).not.toBeNull();
    expect(bar?.getAttribute('aria-hidden')).not.toBeNull();
    expect(bar?.className ?? '').toMatch(/\bw-0\.5\b/);
    expect(bar?.className ?? '').toMatch(/\babsolute\b/);
  });

  it('does not render the ochre bar when not active', () => {
    const { container } = render(
      <BusinessRow business={makeBusiness()} isActive={false} onSelect={vi.fn()} />
    );
    expect(container.querySelector('span.bg-ochre')).toBeNull();
  });

  it('renders 2-letter initials in the avatar', () => {
    render(
      <BusinessRow
        business={makeBusiness({ name: 'Acme Co' })}
        isActive={false}
        onSelect={vi.fn()}
      />
    );
    expect(screen.getByText('AC')).toBeInTheDocument();
  });

  it('renders the business name and role pill', () => {
    render(
      <BusinessRow
        business={makeBusiness({ name: 'Bravo', role: { id: 'r-editor', name: 'editor' } })}
        isActive={false}
        onSelect={vi.fn()}
      />
    );
    expect(screen.getByText('Bravo')).toBeInTheDocument();
    expect(screen.getByText('редактор')).toBeInTheDocument();
  });

  it('calls onSelect with the business when clicked', () => {
    const onSelect = vi.fn();
    const biz = makeBusiness({ id: 'b-2', name: 'Bravo' });
    render(<BusinessRow business={biz} isActive={false} onSelect={onSelect} />);
    fireEvent.click(screen.getByRole('menuitemradio'));
    expect(onSelect).toHaveBeenCalledWith(biz);
  });
});
