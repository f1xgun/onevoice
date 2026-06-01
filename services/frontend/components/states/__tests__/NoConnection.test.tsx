import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { NoConnection } from '../NoConnection';

describe('NoConnection', () => {
  it('renders the calm Russian copy without a status link by default', () => {
    render(<NoConnection />);
    expect(
      screen.getByRole('heading', { name: /Не удаётся связаться с OneVoice/ })
    ).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Открыть статус' })).not.toBeInTheDocument();
  });

  it('renders the status link only when statusUrl is provided', () => {
    render(<NoConnection statusUrl="https://status.example.com" />);
    expect(screen.getByRole('link', { name: 'Открыть статус' })).toHaveAttribute(
      'href',
      'https://status.example.com'
    );
  });

  it('calls onRetry when the primary button is clicked', async () => {
    const onRetry = vi.fn();
    const user = userEvent.setup();
    render(<NoConnection onRetry={onRetry} />);
    await user.click(screen.getByRole('button', { name: 'Попробовать снова' }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('renders the supplied error code in mono', () => {
    render(<NoConnection code="NET_TIMEOUT_5xx" />);
    expect(screen.getByText(/NET_TIMEOUT_5xx/)).toBeInTheDocument();
  });
});
