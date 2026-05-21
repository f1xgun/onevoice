import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ToolCard } from '../ToolCard';
import type { ToolCall } from '@/types/chat';

// Phase D3 follow-up — covers the FE rendering of the i18n catalog key the
// orchestrator stamps on tool_call / tool_result SSE frames. Contract:
//   - When `displayNameKey` is present AND resolves under the
//     `agentTasks.displayName.*` namespace, render the localized label.
//   - When the key is missing OR resolves to the raw key (next-intl's
//     missing-key behavior), fall back to the legacy `tool.name`.
//   - When `displayNameKey` is undefined entirely (older orchestrator
//     deploys), fall back to `tool.name`.
//
// The locale-switch hook `__setTestLocale` is wired in vitest.setup.ts.
type SetLocale = (l: 'ru' | 'en') => void;
declare const __setTestLocale: SetLocale;

function makePending(overrides: Partial<ToolCall> = {}): ToolCall {
  return {
    id: 'd1',
    name: 'telegram__send_channel_post',
    args: { chat_id: 1, text: 'hi' },
    status: 'pending',
    ...overrides,
  };
}

describe('ToolCard — displayNameKey rendering', () => {
  it('Z1: renders the RU localized name when displayNameKey resolves under agentTasks.displayName.*', () => {
    render(
      <ToolCard tool={makePending({ displayNameKey: 'tools.telegram.send_channel_post.name' })} />
    );
    // ru.json — agentTasks.displayName.tools.telegram.send_channel_post.name
    expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    // The raw tool name MUST NOT appear when the localized name was found.
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('Z2: renders the EN localized name after switching locale', () => {
    __setTestLocale('en');
    render(
      <ToolCard tool={makePending({ displayNameKey: 'tools.telegram.send_channel_post.name' })} />
    );
    // en.json — agentTasks.displayName.tools.telegram.send_channel_post.name
    expect(screen.getByText('Send post')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('Z3: falls back to tool.name when displayNameKey is undefined (older orchestrator deploy)', () => {
    render(<ToolCard tool={makePending({ displayNameKey: undefined })} />);
    expect(screen.getByText('telegram__send_channel_post')).toBeInTheDocument();
  });

  it('Z4: falls back to tool.name when displayNameKey is the empty string (defensive guard)', () => {
    // Backend serializers occasionally surface `""` instead of dropping
    // the field. The component's truthy guard treats that the same as
    // undefined and renders the raw tool name.
    render(<ToolCard tool={makePending({ displayNameKey: '' })} />);
    expect(screen.getByText('telegram__send_channel_post')).toBeInTheDocument();
  });

  it('Z5: preserves the strike-through class on the localized name for rejected status', () => {
    render(
      <ToolCard
        tool={makePending({
          status: 'rejected',
          displayNameKey: 'tools.telegram.send_channel_post.name',
          rejectReason: 'no thanks',
        })}
      />
    );
    const nameNode = screen.getByText('Отправить пост');
    expect(nameNode.className).toMatch(/\bline-through\b/);
    expect(nameNode.className).toMatch(/\btext-muted-foreground\b/);
  });
});
