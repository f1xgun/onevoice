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

  it('enabled leaf — checkbox uses the readable description', () => {
    renderLeaf({ leafName: 'business.update' });
    expect(screen.getByLabelText('Редактировать профиль организации')).toBeInTheDocument();
  });

  it('disabled leaf — explains why it cannot be changed', () => {
    renderLeaf({ disabled: true, actorHas: false });
    expect(screen.getByText('У вас нет этого права')).toBeInTheDocument();
  });

  it('renders the readable description and no permission key', () => {
    renderLeaf({ leafName: 'business.update' });
    expect(screen.getByText('Редактировать профиль организации')).toBeInTheDocument();
    expect(screen.queryByText('business.update')).not.toBeInTheDocument();
  });
});
