import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { VKCommunityModal } from '../VKCommunityModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const apiPost = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    post: (...args: unknown[]) => apiPost(...args),
  },
}));

function Wrapper({ client, children }: { client: QueryClient; children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderModal(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <Wrapper client={client}>
      <VKCommunityModal open={true} onClose={onClose} />
    </Wrapper>
  );
  return { ...utils, onClose };
}

describe('VKCommunityModal — paste flow', () => {
  beforeEach(() => {
    apiPost.mockReset();
    vi.mocked(toast.success).mockReset();
    vi.mocked(toast.error).mockReset();
  });

  it('renders the title and the inline instructions', () => {
    renderModal();
    expect(screen.getByText('Подключить сообщество VK')).toBeInTheDocument();
    expect(screen.getByText(/Где взять ключ/)).toBeInTheDocument();
    expect(screen.getByText(/Не выбирайте приложение/)).toBeInTheDocument();
  });

  it('disables Submit when the textarea is empty or whitespace-only', async () => {
    const user = userEvent.setup();
    renderModal();
    const submit = screen.getByRole('button', { name: /^Подключить$/ });
    expect(submit).toBeDisabled();

    const textarea = screen.getByLabelText('Ключ доступа сообщества');
    await user.type(textarea, '   ');
    expect(submit).toBeDisabled();

    await user.type(textarea, 'vk1.a.test');
    expect(submit).not.toBeDisabled();
  });

  it('POSTs the trimmed token to /integrations/vk/connect on submit', async () => {
    apiPost.mockResolvedValueOnce({ data: { id: 'int-1', externalId: '236912172' } });
    const user = userEvent.setup();
    const { onClose } = renderModal();

    const textarea = screen.getByLabelText('Ключ доступа сообщества');
    await user.type(textarea, '   vk1.a.SOME_TOKEN   ');
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    await waitFor(() => expect(apiPost).toHaveBeenCalledTimes(1));
    expect(apiPost).toHaveBeenCalledWith('/integrations/vk/connect', {
      access_token: 'vk1.a.SOME_TOKEN',
    });
    expect(toast.success).toHaveBeenCalledWith('Сообщество VK подключено');
    expect(onClose).toHaveBeenCalled();
  });

  it('surfaces API error messages via toast and keeps the modal open', async () => {
    apiPost.mockRejectedValueOnce({
      response: {
        data: {
          error:
            'токену не хватает прав на «Стену» — пересоздайте ключ в админке сообщества с галочкой «Стена»',
        },
      },
    });
    const user = userEvent.setup();
    const { onClose } = renderModal();

    await user.type(screen.getByLabelText('Ключ доступа сообщества'), 'vk1.a.SOME');
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    const msg = vi.mocked(toast.error).mock.calls[0]?.[0];
    expect(String(msg)).toContain('Стен');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('falls back to a generic error when the API response carries no message', async () => {
    apiPost.mockRejectedValueOnce(new Error('network'));
    const user = userEvent.setup();
    renderModal();

    await user.type(screen.getByLabelText('Ключ доступа сообщества'), 'vk1.a.X');
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Не удалось подключить сообщество'));
  });
});
