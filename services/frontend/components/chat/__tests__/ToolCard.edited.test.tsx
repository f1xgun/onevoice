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
    // Pencil icon carries aria-label so SR users receive the tooltip text even
    // without a hover event — we assert the label directly.
    const label = screen.getByLabelText('Аргументы изменены пользователем');
    expect(label).toBeInTheDocument();
  });

  it('ZZ bis: tooltip-wrapped Pencil preserves the existing green check for a done + edited tool', () => {
    render(<ToolCard tool={makeDone({ wasEdited: true })} />);
    // Existing done branch continues to render the ✅ glyph.
    expect(screen.getByText('✅')).toBeInTheDocument();
    // And the Pencil aria-label is still present alongside.
    expect(screen.getByLabelText('Аргументы изменены пользователем')).toBeInTheDocument();
  });

  it('AAA: done + !wasEdited renders no Pencil icon / edited label', () => {
    render(<ToolCard tool={makeDone({ wasEdited: false })} />);
    expect(screen.queryByLabelText('Аргументы изменены пользователем')).not.toBeInTheDocument();
  });

  it('BBB: existing pending/done/error/aborted branches remain unchanged', () => {
    const { rerender, container } = render(
      <ToolCard
        tool={{
          id: 'p1',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'pending',
        }}
      />
    );
    // Pending: spinner span with border-t-blue-500.
    expect(container.querySelector('.border-t-blue-500')).not.toBeNull();

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
    expect(screen.getByText('✅')).toBeInTheDocument();

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
    expect(screen.getByText('❌')).toBeInTheDocument();
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
    expect(screen.getByText('⏸')).toBeInTheDocument();
    expect(screen.getByText('Выполнение прервано — результат не получен')).toBeInTheDocument();
  });
});
