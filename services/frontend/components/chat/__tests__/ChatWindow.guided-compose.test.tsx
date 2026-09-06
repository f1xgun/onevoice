import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { ChatWindow } from '../ChatWindow';
import { useConversationFlow } from '@/hooks/useConversationFlow';
import { useBusinessStore } from '@/lib/stores/business';
import { singleCallBatch } from '@/test-utils/pending-approval-fixtures';
import type { PendingApproval } from '@/types/chat';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false }),
}));

// Inject a spy sendMessage so the test asserts the exact composed string the
// guided-compose picker seeds into the EXISTING chat loop. pendingApproval is
// parameterised so one case proves the existing HITL card still renders.
const sendMessage = vi.fn();
let pendingApproval: PendingApproval | null = null;
vi.mock('@/hooks/useConversationFlow', () => ({
  useConversationFlow: vi.fn(),
}));

// Trim the empty-state to the surface under test: the checklist/wizard/help
// pull their own query chains that are irrelevant here.
vi.mock('@/components/onboarding/GettingStartedChecklist', () => ({
  GettingStartedChecklist: () => null,
}));
vi.mock('@/components/onboarding/FirstActionWizard', () => ({
  FirstActionWizard: () => null,
}));
vi.mock('@/components/onboarding/SectionHelp', () => ({
  SectionHelp: () => null,
}));

// A connected channel so the empty-state shows the chips + compose picker
// rather than the connect nudge.
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: (url: string) => {
      if (url.includes('/integrations')) {
        return Promise.resolve({ data: [{ platform: 'telegram', status: 'active' }] });
      }
      return Promise.resolve({ data: { id: 'conv-1', title: 'Test', projectId: null } });
    },
  }),
}));

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 } } });
}

function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>;
}

function flowReturn() {
  return {
    messages: [],
    isLoading: false,
    isStreaming: false,
    awaitingTurn: false,
    sendMessage,
    stop: vi.fn(),
    pendingApproval,
    resolveApproval: vi.fn(),
    isResolving: false,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  pendingApproval = null;
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
  vi.mocked(useConversationFlow).mockImplementation(() => flowReturn());
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ChatWindow — guided compose seeds the existing send path', () => {
  it('reaches compose from the empty-state and calls sendMessage with the composed string', async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    const trigger = await screen.findByRole('button', { name: 'Составить пост' });
    await user.click(trigger);

    await user.type(screen.getByLabelText('О чём пост'), 'открытие в субботу');
    await user.click(screen.getByRole('button', { name: 'Подготовить в чате' }));

    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith(
      'Напиши анонс для организации на тему: открытие в субботу. Составь готовый пост.'
    );
  });

  it('renders the existing ToolApprovalCard when a publish tool call is pending (HITL unchanged)', async () => {
    pendingApproval = singleCallBatch;
    render(
      <Wrapper>
        <ChatWindow conversationId="conv-1" />
      </Wrapper>
    );

    expect(
      await screen.findByRole('region', { name: /Ожидает подтверждения/ })
    ).toBeInTheDocument();
  });
});

it('does not scroll away from a reader and resumes following near the end', () => {
  const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
  const client = makeClient();
  function view() {
    return (
      <QueryClientProvider client={client}>
        <ChatWindow conversationId="conv-1" />
      </QueryClientProvider>
    );
  }
  const { container, rerender } = render(view());
  const feed = container.querySelector('.overflow-y-auto')!;
  Object.defineProperties(feed, {
    scrollHeight: { configurable: true, value: 2000 },
    clientHeight: { configurable: true, value: 500 },
    scrollTop: { configurable: true, writable: true, value: 200 },
  });
  fireEvent.scroll(feed);
  scroll.mockClear();
  rerender(view());
  expect(scroll).not.toHaveBeenCalled();
  feed.scrollTop = 1500;
  fireEvent.scroll(feed);
  rerender(view());
  expect(scroll).toHaveBeenCalled();
});

it.skipIf(!hasLayoutBrowser)(
  'keeps long approval text and its final action reachable across widths and locales',
  async () => {
    for (const locale of ['ru', 'en'] as const) {
      (globalThis as unknown as { __setTestLocale: (locale: string) => void }).__setTestLocale(
        locale
      );
      pendingApproval = {
        ...singleCallBatch,
        calls: singleCallBatch.calls.map((call) => ({
          ...call,
          args: { ...call.args, text: 'Ёё Йй Щщ ₽ № Long draft. '.repeat(100) },
        })),
      };
      const { container, unmount } = render(
        <Wrapper>
          <div className="h-dvh">
            <ChatWindow conversationId="conv-1" />
          </div>
        </Wrapper>
      );
      await userEvent.click(
        screen.getByRole('button', {
          name: locale === 'ru' ? /^Изменить telegram/ : /^Edit telegram/,
        })
      );
      for (const width of [320, 375, 768, 1024, 1440]) {
        for (const theme of ['light', 'dark']) {
          await withLayoutPage(
            `<div class="${theme}">${container.innerHTML}</div>`,
            { width, height: 740 },
            async (page) => {
              expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
              const text = page.locator('textarea:not([disabled])').first();
              await text.scrollIntoViewIfNeeded();
              const bounds = await text.boundingBox();
              expect(bounds!.width).toBeGreaterThan(Math.min(width - 150, 400));
              const finalAction = page.getByRole('button', {
                name: locale === 'ru' ? 'Подтвердить' : 'Confirm',
                exact: true,
              });
              await finalAction.scrollIntoViewIfNeeded();
              const actionBounds = await finalAction.boundingBox();
              expect(actionBounds!.y).toBeGreaterThanOrEqual(0);
              expect(actionBounds!.y + actionBounds!.height).toBeLessThanOrEqual(740);
            }
          );
        }
      }
      unmount();
    }
  },
  60000
);
