import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ToolCallsBlock } from '../ToolCallsBlock';
import type { ToolCall } from '@/types/chat';

it.each(['ru', 'en'] as const)(
  'keeps approval and unknown outcomes out of confirmed results in %s',
  async (locale) => {
    (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
      locale
    );
    const calls: ToolCall[] = ['done', 'aborted', 'rejected', 'pending'].map((status, index) => ({
      id: String(index),
      name: 'telegram__send_channel_post',
      args: {},
      status: status as ToolCall['status'],
    }));
    render(<ToolCallsBlock toolCalls={calls} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.getByLabelText(locale === 'ru' ? 'Готово' : 'Done')).toBeVisible();
    expect(
      screen.getByLabelText(locale === 'ru' ? 'Исход неизвестен' : 'Outcome unknown')
    ).toBeVisible();
    expect(screen.queryByText(locale === 'ru' ? 'Отправлено' : 'Sent')).not.toBeInTheDocument();
  }
);

it('folds successful read calls while keeping mutation outcomes visible', async () => {
  const calls: ToolCall[] = [
    {
      id: 'read',
      name: 'telegram__get_comments',
      args: {},
      result: { comments: [] },
      status: 'done',
    },
    { id: 'write', name: 'telegram__send_channel_post', args: {}, status: 'aborted' },
  ];
  render(<ToolCallsBlock toolCalls={calls} />);
  const toggle = screen.getByRole('button');
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
  expect(screen.getByLabelText('Исход неизвестен')).toBeVisible();
  await userEvent.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByText('Ничего не нашлось')).toBeInTheDocument();
});
