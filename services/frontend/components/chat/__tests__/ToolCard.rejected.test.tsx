import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ToolCard } from '../ToolCard';
import type { ToolCall } from '@/types/chat';

function makeRejected(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'r1',
    name: 'telegram__send_channel_post',
    args: { chat_id: 1, text: 'hi' },
    status: 'rejected',
    rejectReason: 'no thanks',
    ...overrides,
  };
}

describe('ToolCard — rejected', () => {
  it("RR: renders the 'Отклонено пользователем' badge when status === 'rejected'", () => {
    render(<ToolCard tool={makeRejected()} />);
    expect(screen.getByText('Отклонено пользователем')).toBeInTheDocument();
  });

  it('SS: rejected tool name carries line-through + text-muted-foreground classes', () => {
    render(<ToolCard tool={makeRejected()} />);
    const nameNode = screen.getByText('telegram__send_channel_post');
    expect(nameNode.className).toMatch(/\bline-through\b/);
    expect(nameNode.className).toMatch(/\btext-muted-foreground\b/);
  });

  it('TT: rejected wrapper overrides borderLeftColor to hsl(var(--destructive))', () => {
    const { container } = render(<ToolCard tool={makeRejected()} />);
    const wrapper = container.firstElementChild as HTMLElement | null;
    expect(wrapper).not.toBeNull();
    const styleAttr = wrapper?.getAttribute('style') ?? '';
    expect(styleAttr).toContain('hsl(var(--destructive))');
  });

  it('UU: rejected + rejectReason renders "Причина: {reason}" line', () => {
    render(<ToolCard tool={makeRejected({ rejectReason: 'no thanks' })} />);
    expect(screen.getByText(/Причина:\s*no thanks/)).toBeInTheDocument();
  });

  it('VV: rejected without rejectReason renders no "Причина:" line', () => {
    render(<ToolCard tool={makeRejected({ rejectReason: undefined })} />);
    expect(screen.queryByText(/Причина:/)).not.toBeInTheDocument();
  });
});
