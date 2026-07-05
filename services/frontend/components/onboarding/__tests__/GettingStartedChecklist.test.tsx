import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('@/lib/stores/business', () => ({ useBusinessStore: vi.fn() }));
vi.mock('@/hooks/useOnboardingProgress', () => ({ useOnboardingProgress: vi.fn() }));

import { useBusinessStore } from '@/lib/stores/business';
import {
  useOnboardingProgress,
  type OnboardingProgress,
  type OnboardingStep,
} from '@/hooks/useOnboardingProgress';
import { GettingStartedChecklist } from '@/components/onboarding/GettingStartedChecklist';
import { gettingStartedDismissKey } from '@/lib/onboarding/dismiss';

/* eslint-disable @typescript-eslint/no-explicit-any */

const STEPS: OnboardingStep[] = [
  { id: 'createOrg', href: '/business', done: true, loading: false, gating: true },
  { id: 'connectChannel', href: '/integrations', done: false, loading: false, gating: true },
  { id: 'describeOrg', href: '/business', done: false, loading: false, gating: true },
  { id: 'firstAction', href: '/chat', done: false, loading: false, gating: true },
];

function makeProgress(over: Partial<OnboardingProgress> = {}): OnboardingProgress {
  return {
    steps: STEPS,
    completedCount: 1,
    total: 4,
    allDone: false,
    loaded: true,
    ...over,
  };
}

const KEY = gettingStartedDismissKey('biz-1');

beforeEach(() => {
  localStorage.clear();
  vi.mocked(useBusinessStore).mockImplementation((sel: any) =>
    sel({ activeBusinessId: 'biz-1', setActive: vi.fn(), clear: vi.fn() })
  );
  vi.mocked(useOnboardingProgress).mockReturnValue(makeProgress());
});

describe('GettingStartedChecklist — rendering + progress', () => {
  it('renders the title, step labels and the progressbar reflecting the count', () => {
    render(<GettingStartedChecklist />);
    expect(screen.getByText('С чего начать')).toBeInTheDocument();
    expect(screen.getByText('Подключить канал')).toBeInTheDocument();
    expect(screen.getByText('Готово 1 из 4')).toBeInTheDocument();

    const bar = screen.getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '1');
    expect(bar).toHaveAttribute('aria-valuemax', '4');
  });

  it('a done step shows no CTA; a todo step deep-links to its route', () => {
    render(<GettingStartedChecklist />);
    // createOrg is done → no CTA link named after its cta.
    expect(screen.queryByRole('link', { name: 'Открыть' })).not.toBeInTheDocument();
    // connectChannel is todo → CTA link to /integrations.
    const link = screen.getByRole('link', { name: 'Подключить' });
    expect(link).toHaveAttribute('href', '/integrations');
  });
});

describe('GettingStartedChecklist — dismiss + persistence', () => {
  it('dismiss hides the card and persists the per-business key', async () => {
    const user = userEvent.setup();
    render(<GettingStartedChecklist />);
    await user.click(screen.getByRole('button', { name: 'Скрыть подсказку' }));
    expect(screen.queryByText('С чего начать')).not.toBeInTheDocument();
    expect(localStorage.getItem(KEY)).toBe('1');
  });

  it('stays hidden when the dismiss key is already set', () => {
    localStorage.setItem(KEY, '1');
    render(<GettingStartedChecklist />);
    expect(screen.queryByText('С чего начать')).not.toBeInTheDocument();
  });

  it('auto-hides and persists dismiss once every gating step is done', async () => {
    vi.mocked(useOnboardingProgress).mockReturnValue(
      makeProgress({ completedCount: 4, allDone: true })
    );
    render(<GettingStartedChecklist />);
    expect(screen.queryByText('С чего начать')).not.toBeInTheDocument();
    await waitFor(() => expect(localStorage.getItem(KEY)).toBe('1'));
  });
});

describe('GettingStartedChecklist — page mode (dismissible=false)', () => {
  it('shows a completed state instead of unmounting and does not persist dismiss', () => {
    vi.mocked(useOnboardingProgress).mockReturnValue(
      makeProgress({ completedCount: 4, allDone: true })
    );
    render(<GettingStartedChecklist dismissible={false} />);
    expect(screen.getByText('Всё готово')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Скрыть подсказку' })).not.toBeInTheDocument();
    expect(localStorage.getItem(KEY)).toBeNull();
  });
});

describe('GettingStartedChecklist — wizard seam', () => {
  it('firstAction deep-links to /chat by default', () => {
    render(<GettingStartedChecklist />);
    const start = screen.getByRole('link', { name: 'Начать' });
    expect(start).toHaveAttribute('href', '/chat');
  });

  it('firstAction opens the wizard handler when onOpenWizard is provided (no new flag)', async () => {
    const onOpenWizard = vi.fn();
    const user = userEvent.setup();
    render(<GettingStartedChecklist onOpenWizard={onOpenWizard} />);
    // Now a button, not a link — the wizard mounts via the handler.
    expect(screen.queryByRole('link', { name: 'Начать' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Начать' }));
    expect(onOpenWizard).toHaveBeenCalledTimes(1);
  });
});
