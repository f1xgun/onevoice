import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import GettingStartedPage from '../page';
import { trackEvent } from '@/lib/telemetry';

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('@/components/ui/page-header', () => ({
  PageHeader: () => null,
}));

// Reduce the checklist to its firstAction seam: a button that invokes the
// onOpenWizard prop the page passes in. This is exactly the handler
// GettingStartedChecklist exposes on that step when onOpenWizard is provided.
vi.mock('@/components/onboarding/GettingStartedChecklist', () => ({
  GettingStartedChecklist: ({ onOpenWizard }: { onOpenWizard?: () => void }) => (
    <button type="button" onClick={onOpenWizard}>
      first-action-cta
    </button>
  ),
}));

// Stub the controlled wizard so the test asserts the open state the page owns,
// without pulling the wizard's own query chain into this fixture.
vi.mock('@/components/onboarding/FirstActionWizard', () => ({
  FirstActionWizard: ({ open }: { open: boolean; onClose: () => void }) =>
    open ? <div data-testid="first-action-wizard">wizard-open</div> : null,
}));

describe('GettingStartedPage — first-action wizard wiring', () => {
  beforeEach(() => {
    vi.mocked(trackEvent).mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('the checklist firstAction CTA opens the FirstActionWizard via onOpenWizard', async () => {
    const user = userEvent.setup();
    render(<GettingStartedPage />);

    const cta = screen.getByRole('button', { name: 'first-action-cta' });
    expect(screen.queryByTestId('first-action-wizard')).not.toBeInTheDocument();

    await user.click(cta);

    expect(screen.getByTestId('first-action-wizard')).toBeInTheDocument();
  });

  it('emits an activation/open_wizard telemetry event on the CTA click', async () => {
    const user = userEvent.setup();
    render(<GettingStartedPage />);

    await user.click(screen.getByRole('button', { name: 'first-action-cta' }));

    expect(trackEvent).toHaveBeenCalledWith('activation', 'open_wizard', {
      metadata: { source: 'getting_started_page' },
    });
  });
});
