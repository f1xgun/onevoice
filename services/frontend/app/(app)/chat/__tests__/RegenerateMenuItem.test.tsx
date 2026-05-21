import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import type { ReactNode } from 'react';

import { ConversationItem } from '@/components/chat/ConversationItem';

// "Обновить заголовок" menu item:
//
//   - Visible when titleStatus !== 'manual'.
//   - Click invokes the regenerate-title mutation.
//   - 409 server response surfaces the verbatim Russian copy via toast.error.
//
// The toast actually rendered in the DOM is asserted to contain the locked
// Russian copy, byte-exact:
//   "Нельзя регенерировать — вы уже переименовали чат вручную"
//   "Заголовок уже генерируется"
//
// Approach: real <Toaster /> mounted; `vi.spyOn(api, 'post')` returns a
// rejected axios-shaped error matching the API's 409 body. The mutation's
// onError surfaces err.response.data.message via toast.error → DOM. We then
// findByText the verbatim Russian string. (msw is not in package.json — the
// vi.spyOn fallback is fully equivalent for the locked-copy contract.)

const BUSINESS_ID = 'biz-test';

// Controls for per-test override of bizApi responses.
const bizApiGet = vi.fn();
const bizApiPost = vi.fn();

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({ get: bizApiGet, post: bizApiPost }),
}));

// Stub MoveChatMenuItem's projects fetch so dropdown rendering does not
// throw when the kebab is opened.
vi.mock('@/hooks/useProjects', () => ({
  useProjectsQuery: () => ({ data: [], isLoading: false }),
}));

vi.mock('@/hooks/useConversations', () => ({
  useMoveConversation: () => ({ mutate: vi.fn() }),
  conversationsQueryKey: (bizId: string) => ['businesses', bizId, 'conversations'] as const,
}));

vi.mock('@/lib/telemetry', () => ({
  trackClick: vi.fn(),
  trackEvent: vi.fn(),
}));

// next/navigation is unavailable inside vitest's jsdom — ChatListPage uses
// useRouter() at mount, so we stub it.
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/chat',
  useSearchParams: () => new URLSearchParams(),
}));

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 }, mutations: { retry: false } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return (
    <QueryClientProvider client={client}>
      {children}
      {/* Real Toaster — toast.error renders text into the DOM that
          screen.findByText can locate. */}
      <Toaster />
    </QueryClientProvider>
  );
}

interface TestConv {
  id: string;
  title: string;
  titleStatus?: 'auto_pending' | 'auto' | 'manual';
  createdAt: string;
  projectId?: string | null;
}

const baseAuto: TestConv = {
  id: 'c-1',
  title: 'Existing auto title',
  titleStatus: 'auto',
  createdAt: '2026-04-26T10:00:00Z',
  projectId: null,
};

function renderItem(conv: TestConv, onRegenerateTitle: () => void = vi.fn()) {
  return render(
    <Wrapper>
      <ConversationItem
        conv={conv}
        onOpen={vi.fn()}
        onRename={vi.fn()}
        onDelete={vi.fn()}
        onRegenerateTitle={onRegenerateTitle}
      />
    </Wrapper>
  );
}

describe('"Обновить заголовок" menu item visibility', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("is visible when titleStatus === 'auto'", async () => {
    const user = userEvent.setup();
    renderItem({ ...baseAuto, titleStatus: 'auto' });
    // Open kebab.
    await user.click(screen.getByRole('button', { name: /Меню чата/ }));
    // Item rendered with verbatim Russian label.
    expect(await screen.findByText('Обновить заголовок')).toBeInTheDocument();
  });

  it("is visible when titleStatus === 'auto_pending'", async () => {
    const user = userEvent.setup();
    renderItem({ ...baseAuto, titleStatus: 'auto_pending' });
    await user.click(screen.getByRole('button', { name: /Меню чата/ }));
    expect(await screen.findByText('Обновить заголовок')).toBeInTheDocument();
  });

  it("is HIDDEN when titleStatus === 'manual'", async () => {
    const user = userEvent.setup();
    renderItem({ ...baseAuto, titleStatus: 'manual', title: 'My manual title' });
    await user.click(screen.getByRole('button', { name: /Меню чата/ }));
    // Wait for the dropdown to render so the absence assertion is meaningful:
    // 'Переименовать' is a sibling item that renders unconditionally.
    expect(await screen.findByText('Переименовать')).toBeInTheDocument();
    // Now confirm the regen item is NOT in the DOM.
    expect(screen.queryByText('Обновить заголовок')).not.toBeInTheDocument();
  });

  it('calls onRegenerateTitle on click', async () => {
    const user = userEvent.setup();
    const onRegenerateTitle = vi.fn();
    renderItem({ ...baseAuto, titleStatus: 'auto' }, onRegenerateTitle);
    await user.click(screen.getByRole('button', { name: /Меню чата/ }));
    const item = await screen.findByText('Обновить заголовок');
    await user.click(item);
    expect(onRegenerateTitle).toHaveBeenCalledTimes(1);
  });
});

// ----------------------------------------------------------------------
// Verbatim Russian 409 copy is asserted via toast.findByText after a
// stubbed axios error matching the API's 409 body shape. The user-visible
// Russian copy reaching the user is the test's load-bearing assertion.
// ----------------------------------------------------------------------
describe('regenerateTitle 409 → verbatim Russian toast', () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  it('toasts the verbatim Russian body when server returns 409 title_is_manual', async () => {
    // bizApi().get → returns the conversations list on mount.
    bizApiGet.mockResolvedValue({
      data: [
        {
          id: 'c-conflict',
          title: 'Some auto title',
          titleStatus: 'auto',
          createdAt: '2026-04-26T10:00:00Z',
          projectId: null,
        },
      ],
    });

    // bizApi().post → rejects with the locked Russian body for the
    // regenerate-title call; resolves successfully for any create-conversation call.
    bizApiPost.mockRejectedValueOnce({
      isAxiosError: true,
      response: {
        status: 409,
        data: {
          error: 'title_is_manual',
          message: 'Нельзя регенерировать — вы уже переименовали чат вручную',
        },
      },
    });

    const ChatListPage = (await import('../page')).default;
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatListPage />
      </Wrapper>
    );

    // Wait for the row.
    await screen.findByText('Some auto title');

    // Open the kebab — the trigger button is the only icon-only button next
    // to the row content. We locate it via getAllByRole('button') and pick
    // the unnamed (icon-only) trigger.
    const buttons = screen.getAllByRole('button');
    const trigger = buttons.find((b) => b.getAttribute('aria-haspopup') === 'menu');
    expect(trigger).toBeDefined();
    await user.click(trigger!);

    const item = await screen.findByText('Обновить заголовок');
    await user.click(item);

    // The toast renders the verbatim Russian copy in the DOM.
    await waitFor(
      async () => {
        expect(
          await screen.findByText('Нельзя регенерировать — вы уже переименовали чат вручную')
        ).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });

  it('toasts the verbatim Russian body when server returns 409 title_in_flight', async () => {
    bizApiGet.mockResolvedValue({
      data: [
        {
          id: 'c-inflight',
          title: 'Another auto title',
          titleStatus: 'auto',
          createdAt: '2026-04-26T10:00:00Z',
          projectId: null,
        },
      ],
    });

    bizApiPost.mockRejectedValueOnce({
      isAxiosError: true,
      response: {
        status: 409,
        data: {
          error: 'title_in_flight',
          message: 'Заголовок уже генерируется',
        },
      },
    });

    const ChatListPage = (await import('../page')).default;
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ChatListPage />
      </Wrapper>
    );

    await screen.findByText('Another auto title');

    const buttons = screen.getAllByRole('button');
    const trigger = buttons.find((b) => b.getAttribute('aria-haspopup') === 'menu');
    expect(trigger).toBeDefined();
    await user.click(trigger!);

    const item = await screen.findByText('Обновить заголовок');
    await user.click(item);

    await waitFor(
      async () => {
        expect(await screen.findByText('Заголовок уже генерируется')).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
  });
});
