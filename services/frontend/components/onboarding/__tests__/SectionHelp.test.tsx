import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('@/lib/stores/business', () => ({ useBusinessStore: vi.fn() }));

import { useBusinessStore } from '@/lib/stores/business';
import { SectionHelp } from '@/components/onboarding/SectionHelp';
import { sectionHelpDismissKey } from '@/lib/onboarding/dismiss';

/* eslint-disable @typescript-eslint/no-explicit-any */

declare const __setTestLocale: (locale: 'ru' | 'en') => void;

beforeEach(() => {
  localStorage.clear();
  vi.mocked(useBusinessStore).mockImplementation((sel: any) =>
    sel({ activeBusinessId: 'biz-1', setActive: vi.fn(), clear: vi.fn() })
  );
});

describe('SectionHelp — first-run callout', () => {
  it('renders the section title + body and a dismiss control (RU)', () => {
    render(<SectionHelp section="integrations" />);
    expect(screen.getByText('Зачем подключать каналы')).toBeInTheDocument();
    expect(screen.getByText(/Подключите каналы, чтобы ИИ мог/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Скрыть подсказку' })).toBeInTheDocument();
  });

  it('renders the business primer', () => {
    render(<SectionHelp section="business" />);
    expect(screen.getByText('Профиль обучает ИИ')).toBeInTheDocument();
  });

  it('renders localized copy in EN', () => {
    __setTestLocale('en');
    render(<SectionHelp section="integrations" />);
    expect(screen.getByText('Why connect channels')).toBeInTheDocument();
  });
});

describe('SectionHelp — dismiss + reopen', () => {
  it('dismiss collapses to a reopen affordance and persists the per-section key', async () => {
    const user = userEvent.setup();
    render(<SectionHelp section="integrations" />);
    await user.click(screen.getByRole('button', { name: 'Скрыть подсказку' }));

    // Callout gone; a compact reopen trigger remains.
    expect(screen.queryByText('Зачем подключать каналы')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Открыть подсказку/ })).toBeInTheDocument();
    expect(localStorage.getItem(sectionHelpDismissKey('integrations', 'biz-1'))).toBe('1');
  });

  it('when already dismissed, renders the reopen popover trigger and re-shows content on open', async () => {
    localStorage.setItem(sectionHelpDismissKey('integrations', 'biz-1'), '1');
    const user = userEvent.setup();
    render(<SectionHelp section="integrations" />);

    expect(screen.queryByText('Зачем подключать каналы')).not.toBeInTheDocument();
    const trigger = screen.getByRole('button', { name: /Открыть подсказку/ });
    await user.click(trigger);
    expect(await screen.findByText('Зачем подключать каналы')).toBeInTheDocument();
  });
});
