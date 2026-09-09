import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ToolCard } from '../ToolCard';
import type { ToolCall } from '@/types/chat';

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
    expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('Z2: renders the EN localized name after switching locale', () => {
    __setTestLocale('en');
    render(
      <ToolCard tool={makePending({ displayNameKey: 'tools.telegram.send_channel_post.name' })} />
    );
    expect(screen.getByText('Send post')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('Z3: derives a localized action name when displayNameKey is undefined (older orchestrator deploy)', () => {
    render(<ToolCard tool={makePending({ displayNameKey: undefined })} />);
    expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
  });

  it('Z4: derives a localized action name when displayNameKey is the empty string (defensive guard)', () => {
    render(<ToolCard tool={makePending({ displayNameKey: '' })} />);
    expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    expect(screen.queryByText('telegram__send_channel_post')).not.toBeInTheDocument();
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

it.each([
  ['yandex_business__update_hours', 'Обновить часы работы', 'Яндекс.Бизнес'],
  ['vk__publish_post', 'Опубликовать пост', 'ВКонтакте'],
  ['telegram__send_channel_post', 'Отправить пост', 'Telegram'],
])('localizes a legacy %s action with a platform logo', (name, action, platform) => {
  const { container } = render(<ToolCard tool={makePending({ name })} />);
  expect(screen.getByText(action)).toBeVisible();
  expect(screen.getByText(platform)).toBeVisible();
  expect(container.querySelector('img[src*="/platforms/"]')).not.toBeNull();
  expect(screen.queryByText(name)).not.toBeInTheDocument();
});

it('uses a readable fallback for unknown actions and missing translation keys', () => {
  render(
    <ToolCard
      tool={makePending({ name: 'other__unknown', displayNameKey: 'missing.translation.name' })}
    />
  );
  expect(screen.getByText('Действие на площадке')).toBeVisible();
  expect(screen.queryByText('other__unknown')).not.toBeInTheDocument();
  expect(screen.queryByText('missing.translation.name')).not.toBeInTheDocument();
});
