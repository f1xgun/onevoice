import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { TooltipProvider } from '@/components/ui/tooltip';

import { LeafCheckbox } from '../LeafCheckbox';

// Task 3 — LeafCheckbox covers (disabled leaves are no-op +
// «У вас нет этого права» tooltip) and (Info icon + description tooltip
// for enabled leaves).

function renderLeaf(props: Partial<React.ComponentProps<typeof LeafCheckbox>> = {}) {
  return render(
    <TooltipProvider delayDuration={0}>
      <LeafCheckbox
        leafName="business.update"
        description="Редактировать профиль"
        checked={false}
        disabled={false}
        actorHas
        onToggle={() => {}}
        {...props}
      />
    </TooltipProvider>
  );
}

describe('LeafCheckbox', () => {
  it('enabled leaf: clicking the checkbox fires onToggle(true)', async () => {
    const onToggle = vi.fn();
    renderLeaf({ onToggle });
    await userEvent.setup().click(screen.getByRole('checkbox'));
    expect(onToggle).toHaveBeenCalledWith(true);
  });

  it('enabled leaf already checked: clicking fires onToggle(false)', async () => {
    const onToggle = vi.fn();
    renderLeaf({ onToggle, checked: true });
    await userEvent.setup().click(screen.getByRole('checkbox'));
    expect(onToggle).toHaveBeenCalledWith(false);
  });

  it('disabled leaf: onToggle is NOT called on click', async () => {
    const onToggle = vi.fn();
    renderLeaf({ disabled: true, actorHas: false, onToggle });
    // Radix Checkbox in disabled state ignores clicks.
    await userEvent.setup().click(screen.getByRole('checkbox'));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('actorHas=false → renders the row with opacity-60 class', () => {
    renderLeaf({ disabled: true, actorHas: false });
    const li = screen.getByRole('listitem');
    expect(li.className).toContain('opacity-60');
  });

  it('actorHas=true → row does NOT carry the opacity-60 class', () => {
    renderLeaf({ actorHas: true });
    const li = screen.getByRole('listitem');
    expect(li.className).not.toContain('opacity-60');
  });

  it('enabled leaf — tooltip trigger aria-label exposes the description', () => {
    renderLeaf({ description: 'Редактировать название' });
    // The aria-label on the tooltip trigger is the description text. Tested
    // via the accessibility tree rather than mounting the portal'd tooltip
    // content — jsdom + Radix portals are flaky.
    expect(screen.getByLabelText('Редактировать название')).toBeInTheDocument();
  });

  it('disabled leaf — tooltip trigger aria-label is «У вас нет этого права»', () => {
    renderLeaf({ disabled: true, actorHas: false });
    expect(screen.getByLabelText('У вас нет этого права')).toBeInTheDocument();
  });

  it('renders the permission key as the leaf label (catalog-driven, never hardcoded)', () => {
    renderLeaf({ leafName: 'novel.action' });
    expect(screen.getByText('novel.action')).toBeInTheDocument();
  });
});
