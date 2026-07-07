import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { VKCommunityPickerModal } from '../VKCommunityPickerModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const BUSINESS_ID = 'biz-1';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

const apiGet = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: (...args: unknown[]) => apiGet(...args),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  }),
}));

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderPicker(onClose = vi.fn()) {
  const utils = render(
    <Wrapper>
      <VKCommunityPickerModal open={true} onClose={onClose} />
    </Wrapper>
  );
  return { ...utils, onClose };
}

const COMMUNITIES = [
  { id: 1, name: 'Cafe', screen_name: 'cafe', photo_50: '', members_count: 12 },
  { id: 2, name: 'Bakery', screen_name: 'bakery', photo_50: '', members_count: 340 },
];

describe('VKCommunityPickerModal', () => {
  const originalLocation = window.location;

  beforeEach(() => {
    apiGet.mockReset();
    vi.mocked(toast.error).mockReset();
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { ...originalLocation, href: '' },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: originalLocation,
    });
  });

  it('renders the community list as selectable radios', async () => {
    apiGet.mockResolvedValueOnce({ data: COMMUNITIES });
    renderPicker();

    await waitFor(() => expect(screen.getByText('Cafe')).toBeInTheDocument());
    expect(screen.getByText('Bakery')).toBeInTheDocument();

    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(2);
    expect(apiGet).toHaveBeenCalledWith('/integrations/vk/communities');
  });

  it('selecting a community and clicking Continue GETs community-auth-url and redirects', async () => {
    apiGet
      .mockResolvedValueOnce({ data: COMMUNITIES })
      .mockResolvedValueOnce({ data: { url: 'https://oauth.vk.com/authorize?group_ids=1' } });
    const user = userEvent.setup();
    renderPicker();

    await waitFor(() => expect(screen.getByText('Cafe')).toBeInTheDocument());
    await user.click(screen.getAllByRole('radio')[0]);
    await user.click(screen.getByRole('button', { name: 'Продолжить' }));

    await waitFor(() =>
      expect(apiGet).toHaveBeenCalledWith('/integrations/vk/community-auth-url?group_id=1')
    );
    await waitFor(() =>
      expect(window.location.href).toBe('https://oauth.vk.com/authorize?group_ids=1')
    );
  });

  it('renders the empty state when no communities come back', async () => {
    apiGet.mockResolvedValueOnce({ data: [] });
    renderPicker();

    await waitFor(() =>
      expect(
        screen.getByText(
          'Сообщества не найдены. Убедитесь, что вы администратор хотя бы одного сообщества.'
        )
      ).toBeInTheDocument()
    );
  });

  it('renders the session-expired state when communities GET rejects (410 Gone)', async () => {
    apiGet.mockRejectedValueOnce({ response: { status: 410 } });
    renderPicker();

    await waitFor(() =>
      expect(
        screen.getByText('Сессия истекла. Пожалуйста, подключите ВКонтакте заново.')
      ).toBeInTheDocument()
    );
  });
});
