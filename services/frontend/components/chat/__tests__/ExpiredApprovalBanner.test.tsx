import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ExpiredApprovalBanner } from '../ExpiredApprovalBanner';

const BANNER_TEXT = 'Эта операция истекла — отправьте новое сообщение, чтобы продолжить.';
const DISMISS_LABEL = 'Закрыть сообщение';

describe('ExpiredApprovalBanner', () => {
  it('KK: renders banner text verbatim', () => {
    render(<ExpiredApprovalBanner />);
    expect(screen.getByText(BANNER_TEXT)).toBeInTheDocument();
  });

  it('LL: exposes a dismiss button with the Russian aria-label', () => {
    render(<ExpiredApprovalBanner />);
    expect(screen.getByRole('button', { name: DISMISS_LABEL })).toBeInTheDocument();
  });

  it('MM: root element declares role="alert"', () => {
    render(<ExpiredApprovalBanner />);
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    expect(alert).toHaveTextContent(BANNER_TEXT);
  });

  it('NN: root element declares aria-live="polite"', () => {
    render(<ExpiredApprovalBanner />);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveAttribute('aria-live', 'polite');
  });

  it('OO: clicking dismiss hides the banner from the DOM', async () => {
    const user = userEvent.setup();
    render(<ExpiredApprovalBanner />);
    expect(screen.getByText(BANNER_TEXT)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: DISMISS_LABEL }));
    expect(screen.queryByText(BANNER_TEXT)).not.toBeInTheDocument();
  });

  it('PP: onDismiss callback fires exactly once when the user clicks X', async () => {
    const onDismiss = vi.fn();
    const user = userEvent.setup();
    render(<ExpiredApprovalBanner onDismiss={onDismiss} />);
    await user.click(screen.getByRole('button', { name: DISMISS_LABEL }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('QQ: root element carries the amber palette utility classes', () => {
    render(<ExpiredApprovalBanner />);
    const alert = screen.getByRole('alert');
    const classes = alert.className.split(/\s+/);
    const amberTokens = ['bg-amber-50', 'border-amber-200', 'text-amber-900'];
    const hit = amberTokens.some((token) => classes.includes(token));
    expect(hit).toBe(true);
  });
});
