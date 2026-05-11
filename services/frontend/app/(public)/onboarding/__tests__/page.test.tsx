import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactNode } from 'react';
import messages from '@/messages/ru.json';

const push = vi.fn();

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
}));

import OnboardingPage from '../page';

function wrap(node: ReactNode) {
  return (
    <NextIntlClientProvider locale="ru" messages={messages}>
      {node}
    </NextIntlClientProvider>
  );
}

beforeEach(() => {
  push.mockReset();
});

describe('OnboardingPage', () => {
  it('renders both cards with locked Russian copy', () => {
    render(wrap(<OnboardingPage />));
    expect(screen.getByText('Создать организацию')).toBeInTheDocument();
    expect(screen.getByText('У меня есть приглашение')).toBeInTheDocument();
  });

  it('Card A links to /business/new', () => {
    render(wrap(<OnboardingPage />));
    const link = screen.getByText('Создать организацию').closest('a');
    expect(link).toHaveAttribute('href', '/business/new');
  });

  it('shows the invalid-format error on a too-short token', async () => {
    render(wrap(<OnboardingPage />));
    fireEvent.change(screen.getByLabelText('Ссылка-приглашение'), {
      target: { value: 'too-short' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Открыть приглашение' }));
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Неверный формат ссылки')
    );
    expect(push).not.toHaveBeenCalled();
  });

  it('routes to /invite/{token} on a valid 43-char base64-url token', async () => {
    render(wrap(<OnboardingPage />));
    const valid = 'A'.repeat(43);
    fireEvent.change(screen.getByLabelText('Ссылка-приглашение'), {
      target: { value: valid },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Открыть приглашение' }));
    await waitFor(() => expect(push).toHaveBeenCalledWith(`/invite/${valid}`));
  });

  it('rejects 43 chars with an illegal character', async () => {
    render(wrap(<OnboardingPage />));
    const invalid = '!' + 'A'.repeat(42);
    fireEvent.change(screen.getByLabelText('Ссылка-приглашение'), {
      target: { value: invalid },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Открыть приглашение' }));
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Неверный формат ссылки')
    );
    expect(push).not.toHaveBeenCalled();
  });
});
