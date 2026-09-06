import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { IntlClientProvider } from '@/components/IntlClientProvider';
import { ConversationItem, type Conversation } from '@/components/chat/ConversationItem';
import en from '@/messages/en.json';
import ru from '@/messages/ru.json';

vi.unmock('next-intl');
vi.mock('@/components/chat/MoveChatMenuItem', () => ({
  MoveChatMenuItem: () => null,
}));

describe('conversation title locale switching', () => {
  it.each([
    {
      name: 'trailing newline',
      title: 'Untitled chat September 6\n',
      titleStatus: 'auto',
      fallback: false,
    },
    { name: 'neutral marker', title: '', titleStatus: 'auto', fallback: true },
    {
      name: 'old RU fallback',
      title: 'Без названия 6 сентября',
      titleStatus: 'auto',
      fallback: true,
    },
    {
      name: 'old EN fallback',
      title: 'Untitled chat September 6',
      titleStatus: 'auto',
      fallback: true,
    },
    {
      name: 'mixed fallback',
      title: 'Untitled chat 6 сентября',
      titleStatus: undefined,
      fallback: true,
    },
    {
      name: 'similar user title',
      title: 'Untitled chat about autumn',
      titleStatus: 'manual',
      fallback: false,
    },
    {
      name: 'manual exact shape',
      title: 'Без названия 6 сентября',
      titleStatus: 'manual',
      fallback: false,
    },
    {
      name: 'model title',
      title: 'План публикаций на сентябрь',
      titleStatus: 'auto',
      fallback: false,
    },
    {
      name: 'similar model title',
      title: 'Untitled chat September 6 ideas',
      titleStatus: 'auto',
      fallback: false,
    },
    {
      name: 'invalid date',
      title: 'Untitled chat February 31',
      titleStatus: 'auto',
      fallback: false,
    },
    { name: 'unknown month', title: 'Без названия 6 осени', titleStatus: 'auto', fallback: false },
    {
      name: 'verbatim whitespace',
      title: '  Мой заголовок  ',
      titleStatus: 'manual',
      fallback: false,
    },
  ] as const)('$name', ({ title, titleStatus, fallback }) => {
    const conv: Conversation = {
      id: 'conversation',
      title,
      titleStatus,
      createdAt: '2026-09-06T12:00:00Z',
    };
    const item = (
      <ConversationItem
        conv={conv}
        onOpen={vi.fn()}
        onRename={vi.fn()}
        onDelete={vi.fn()}
        onRegenerateTitle={vi.fn()}
      />
    );
    const { rerender } = render(
      <IntlClientProvider locale="ru" messages={ru}>
        {item}
      </IntlClientProvider>
    );
    const getTitle = () => screen.getAllByRole('button')[0].querySelector('p')?.textContent;
    expect(getTitle()).toBe(fallback ? 'Чат от 6 сентября' : title);
    rerender(
      <IntlClientProvider locale="en" messages={en}>
        {item}
      </IntlClientProvider>
    );
    expect(getTitle()).toBe(fallback ? 'Chat from 6 September' : title);
    expect(conv.title).toBe(title);
  });
});
