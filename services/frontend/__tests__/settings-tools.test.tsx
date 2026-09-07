import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import { ToolsPageClient } from '@/app/(app)/settings/tools/ToolsPageClient';
import type { Tool } from '@/lib/schemas';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// ToolsPageClient still fetches /business via the bare api client (migrated
// in Task 4). Keep that mock for the business query.
const apiGet = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    put: vi.fn(),
  },
}));

// tools and tool-approvals are now business-scoped via bizApi.
const bizApiGet = vi.fn();
const bizApiPut = vi.fn();

vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => bizApiGet(bizId, path, config),
    post: vi.fn(),
    put: (path: string, data?: unknown, config?: unknown) => bizApiPut(bizId, path, data, config),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

// useTools reads activeBusinessId from the store.
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'biz-1' }),
}));

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderPage() {
  const client = makeClient();
  return render(
    <Wrapper client={client}>
      <ToolsPageClient />
    </Wrapper>
  );
}

function Wrapper({ client, children }: { client: QueryClient; children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const BUSINESS_ID = 'biz-1';
const TELEGRAM_POST: Tool = {
  name: 'telegram__send_channel_post',
  platform: 'telegram',
  floor: 'manual',
  editableFields: ['text'],
  description: 'Send a text post',
};
const TELEGRAM_PHOTO: Tool = {
  name: 'telegram__send_channel_photo',
  platform: 'telegram',
  floor: 'manual',
  editableFields: ['caption'],
  description: 'Send a photo',
};
const VK_PUBLISH: Tool = {
  name: 'vk__publish_post',
  platform: 'vk',
  floor: 'auto',
  editableFields: [],
  description: 'Auto-floor publish',
};
const YANDEX_REPLY: Tool = {
  name: 'yandex_business__reply_review',
  platform: 'yandex_business',
  floor: 'manual',
  editableFields: ['text'],
  description: 'Reply to review',
};
const GOOGLE_REPLY: Tool = {
  name: 'google_business__reply_review',
  platform: 'google_business',
  floor: 'forbidden',
  editableFields: [],
  description: 'Reply to Google review (disabled)',
};

const ALL_TOOLS: Tool[] = [TELEGRAM_POST, TELEGRAM_PHOTO, VK_PUBLISH, YANDEX_REPLY, GOOGLE_REPLY];

function setupDefaultMocks() {
  apiGet.mockImplementation((url: string) => {
    if (url === '/business') return Promise.resolve({ data: { id: BUSINESS_ID, name: 'Test' } });
    return Promise.resolve({ data: null });
  });

  bizApiGet.mockImplementation((bizId: string, path: string) => {
    if (path === '/tools') return Promise.resolve({ data: ALL_TOOLS });
    if (path === '/tool-approvals') {
      return Promise.resolve({
        data: { toolApprovals: { [TELEGRAM_POST.name]: 'manual', [YANDEX_REPLY.name]: 'auto' } },
      });
    }
    return Promise.resolve({ data: null });
  });

  bizApiPut.mockResolvedValue({
    data: {
      toolApprovals: { [TELEGRAM_POST.name]: 'auto', [YANDEX_REPLY.name]: 'auto' },
    },
  });
}

describe('ToolsPageClient — /settings/tools', () => {
  beforeEach(() => {
    apiGet.mockReset();
    bizApiGet.mockReset();
    bizApiPut.mockReset();
    (toast.success as ReturnType<typeof vi.fn>).mockReset();
    (toast.error as ReturnType<typeof vi.fn>).mockReset();
  });

  it('renders the Linen-era Russian page title', async () => {
    setupDefaultMocks();
    renderPage();
    expect(await screen.findByRole('heading', { name: /Что разрешено ИИ/ })).toBeInTheDocument();
  });

  it('shows a switch for manual-floor tools and omits auto-floor tools', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    });
    expect(screen.getByText('Отправить фото')).toBeInTheDocument();
    expect(screen.getByText('Ответить на отзыв Яндекса')).toBeInTheDocument();

    expect(screen.queryByText('Опубликовать пост')).not.toBeInTheDocument();
  });

  it('renders forbidden-floor tools with the «Запрещено» badge and no switch', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Ответить на отзыв Google')).toBeInTheDocument();
    });

    const forbiddenRow = screen.getByText('Ответить на отзыв Google').closest('div');
    expect(forbiddenRow).not.toBeNull();
    expect(screen.getByText('Запрещено')).toBeInTheDocument();
    expect(
      screen.queryByLabelText(`Режим одобрения для Ответить на отзыв Google`)
    ).not.toBeInTheDocument();
  });

  it('Save is disabled until the user toggles something', async () => {
    setupDefaultMocks();
    renderPage();

    const saveBtn = await screen.findByRole('button', { name: /Сохранить/ });
    expect(saveBtn).toBeDisabled();

    const radiogroup = await screen.findByRole('radiogroup', {
      name: `Режим одобрения для Отправить пост`,
    });
    const samBtn = within(radiogroup).getByRole('radio', { name: 'Автоматически' });
    within(radiogroup).getByRole('radio', { checked: true }).focus();
    await userEvent.keyboard('{ArrowRight}');
    expect(samBtn).toHaveFocus();
    expect(samBtn).toHaveAttribute('aria-checked', 'true');

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Сохранить/ })).toBeEnabled();
    });
  });

  it('clicking Save sends the FULL current map for every manual-floor tool (no partial updates) and shows success toast', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Отправить пост')).toBeInTheDocument();
    });

    const radiogroup = screen.getByRole('radiogroup', {
      name: `Режим одобрения для Отправить пост`,
    });
    await userEvent.click(within(radiogroup).getByRole('radio', { name: 'Автоматически' }));

    await userEvent.click(screen.getByRole('button', { name: /Сохранить/ }));

    await waitFor(() => {
      expect(bizApiPut).toHaveBeenCalledTimes(1);
    });

    const [bizId, path, body] = bizApiPut.mock.calls[0]!;
    expect(bizId).toBe(BUSINESS_ID);
    expect(path).toBe('/tool-approvals');
    expect(body).toEqual({
      toolApprovals: {
        [TELEGRAM_POST.name]: 'auto',
        [TELEGRAM_PHOTO.name]: 'manual',
        [YANDEX_REPLY.name]: 'auto',
      },
    });
    expect((body as { toolApprovals: Record<string, string> }).toolApprovals).not.toHaveProperty(
      GOOGLE_REPLY.name
    );

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith('Настройки сохранены');
    });
  });
});
