import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ToolCard } from '../ToolCard';
import type { ToolCall } from '@/types/chat';

function makeDone(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'd1',
    name: 'telegram__send_channel_post',
    args: { chat_id: 1, text: 'hi' },
    status: 'done',
    ...overrides,
  };
}

describe('ToolCard — edited', () => {
  it("ZZ: done + wasEdited === true exposes a Pencil with aria-label 'Аргументы изменены пользователем'", () => {
    render(<ToolCard tool={makeDone({ wasEdited: true })} />);
    const label = screen.getByLabelText('Аргументы изменены пользователем');
    expect(label).toBeInTheDocument();
  });

  it('ZZ bis: tooltip-wrapped Pencil preserves the existing green check for a done + edited tool', () => {
    render(<ToolCard tool={makeDone({ wasEdited: true })} />);
    expect(screen.getByLabelText('Готово')).toBeInTheDocument();
    expect(screen.getByLabelText('Аргументы изменены пользователем')).toBeInTheDocument();
  });

  it('AAA: done + !wasEdited renders no Pencil icon / edited label', () => {
    render(<ToolCard tool={makeDone({ wasEdited: false })} />);
    expect(screen.queryByLabelText('Аргументы изменены пользователем')).not.toBeInTheDocument();
  });

  it('BBB: distinguishes pending, confirmed, failed and unknown outcomes', () => {
    const { rerender } = render(
      <ToolCard
        tool={{
          id: 'p1',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'pending',
        }}
      />
    );
    expect(screen.getByLabelText('Выполняется')).toBeInTheDocument();

    rerender(
      <ToolCard
        tool={{
          id: 'd2',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'done',
        }}
      />
    );
    expect(screen.getByLabelText('Готово')).toBeInTheDocument();

    rerender(
      <ToolCard
        tool={{
          id: 'e2',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'error',
          error: 'boom',
        }}
      />
    );
    expect(screen.getByLabelText('Ошибка')).toBeInTheDocument();
    expect(screen.getByText('boom')).toBeInTheDocument();

    rerender(
      <ToolCard
        tool={{
          id: 'a1',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'aborted',
        }}
      />
    );
    expect(screen.getByLabelText('Исход неизвестен')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Не удалось проверить выполнение. Проверьте площадку перед повторным действием.'
      )
    ).toBeInTheDocument();
  });
});
