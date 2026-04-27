import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ToolCard } from '../ToolCard';
import type { ToolCall } from '@/types/chat';

function makeExpired(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'e1',
    name: 'telegram__send_channel_post',
    args: { chat_id: 1, text: 'hi' },
    status: 'expired',
    ...overrides,
  };
}

describe('ToolCard — expired', () => {
  it("WW: renders the 'Истекло' badge when status === 'expired'", () => {
    render(<ToolCard tool={makeExpired()} />);
    expect(screen.getByText('Истекло')).toBeInTheDocument();
  });

  it('XX: expired tool name carries line-through class', () => {
    render(<ToolCard tool={makeExpired()} />);
    const nameNode = screen.getByText('telegram__send_channel_post');
    expect(nameNode.className).toMatch(/\bline-through\b/);
  });

  it('YY: expired wrapper keeps the platform border color (telegram blue)', () => {
    const { container } = render(<ToolCard tool={makeExpired()} />);
    const wrapper = container.firstElementChild as HTMLElement | null;
    expect(wrapper).not.toBeNull();
    // jsdom normalizes hex inline styles to rgb(...) when round-tripped through
    // getAttribute/style. Assert the platform accent via element.style which
    // preserves the computed form, plus confirm no destructive override leaked.
    expect(wrapper?.style.borderLeftColor).toBe('rgb(42, 171, 238)'); // #2AABEE
    const styleAttr = wrapper?.getAttribute('style') ?? '';
    expect(styleAttr).not.toContain('hsl(var(--destructive))');
  });
});
