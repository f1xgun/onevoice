import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import { ToolsPageClient } from '@/app/(app)/settings/tools/ToolsPageClient';
import type { Tool } from '@/lib/schemas';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// The API client is the single seam we mock. Route every call the page
// makes to a predictable stub so we can watch what PUT body it sends.
const apiGet = vi.fn();
const apiPut = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => apiGet(...args),
    put: (...args: unknown[]) => apiPut(...args),
  },
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
    if (url === '/tools') return Promise.resolve({ data: ALL_TOOLS });
    if (url === `/business/${BUSINESS_ID}/tool-approvals`) {
      return Promise.resolve({
        data: { toolApprovals: { [TELEGRAM_POST.name]: 'manual', [YANDEX_REPLY.name]: 'auto' } },
      });
    }
    return Promise.resolve({ data: null });
  });
  apiPut.mockResolvedValue({
    data: {
      toolApprovals: { [TELEGRAM_POST.name]: 'auto', [YANDEX_REPLY.name]: 'auto' },
    },
  });
}

describe('ToolsPageClient — /settings/tools (POLICY-05)', () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPut.mockReset();
    (toast.success as ReturnType<typeof vi.fn>).mockReset();
    (toast.error as ReturnType<typeof vi.fn>).mockReset();
  });

  it('renders the Russian page title exactly', async () => {
    setupDefaultMocks();
    renderPage();
    expect(
      await screen.findByRole('heading', { name: /«Настройки одобрения инструментов»/ })
    ).toBeInTheDocument();
  });

  it('shows a switch for manual-floor tools and omits auto-floor tools', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(TELEGRAM_POST.name)).toBeInTheDocument();
    });
    expect(screen.getByText(TELEGRAM_PHOTO.name)).toBeInTheDocument();
    expect(screen.getByText(YANDEX_REPLY.name)).toBeInTheDocument();

    // Auto-floor tool must NOT appear — nothing to configure for it.
    expect(screen.queryByText(VK_PUBLISH.name)).not.toBeInTheDocument();
  });

  it('renders forbidden-floor tools with the «Запрещено» badge and no switch', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(GOOGLE_REPLY.name)).toBeInTheDocument();
    });

    const forbiddenRow = screen.getByText(GOOGLE_REPLY.name).closest('div');
    expect(forbiddenRow).not.toBeNull();
    // The forbidden badge is present in the document.
    expect(screen.getByText('Запрещено')).toBeInTheDocument();
    // No switch rendered for the forbidden tool (query by its a11y label).
    expect(
      screen.queryByLabelText(`Режим одобрения для ${GOOGLE_REPLY.name}`)
    ).not.toBeInTheDocument();
  });

  it('Save is disabled until the user toggles something', async () => {
    setupDefaultMocks();
    renderPage();

    const saveBtn = await screen.findByRole('button', { name: /Сохранить/ });
    expect(saveBtn).toBeDisabled();

    await userEvent.click(screen.getByLabelText(`Режим одобрения для ${TELEGRAM_POST.name}`));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Сохранить/ })).toBeEnabled();
    });
  });

  it('clicking Save sends the FULL current map for every manual-floor tool (no partial updates) and shows success toast', async () => {
    setupDefaultMocks();
    renderPage();

    await waitFor(() => {
      expect(screen.getByText(TELEGRAM_POST.name)).toBeInTheDocument();
    });

    // Flip telegram post auto; leave telegram photo untouched (manual by
    // default); yandex reply starts at auto from server.
    await userEvent.click(screen.getByLabelText(`Режим одобрения для ${TELEGRAM_POST.name}`));

    await userEvent.click(screen.getByRole('button', { name: /Сохранить/ }));

    await waitFor(() => {
      expect(apiPut).toHaveBeenCalledTimes(1);
    });

    const [url, body] = apiPut.mock.calls[0]!;
    expect(url).toBe(`/business/${BUSINESS_ID}/tool-approvals`);
    // The body must include EVERY manual-floor tool's current value — the
    // backend replaces the entire map on PUT.
    expect(body).toEqual({
      toolApprovals: {
        [TELEGRAM_POST.name]: 'auto',
        [TELEGRAM_PHOTO.name]: 'manual',
        [YANDEX_REPLY.name]: 'auto',
      },
    });
    // Forbidden tool must not leak into the payload.
    expect((body as { toolApprovals: Record<string, string> }).toolApprovals).not.toHaveProperty(
      GOOGLE_REPLY.name
    );

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith('Настройки сохранены');
    });
  });
});
