import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { GuidedCompose } from '@/components/onboarding/GuidedCompose';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

const state = vi.hoisted(() => ({
  registry: [
    { id: 'telegram', fullLabel: 'Telegram', status: 'active' },
    { id: 'vk', fullLabel: 'VK', status: 'active' },
    { id: 'yandex_business', fullLabel: 'Яндекс Бизнес', status: 'active' },
    { id: 'google_business', fullLabel: 'Google Business', status: 'active' },
    { id: 'avito', fullLabel: 'Avito', status: 'coming_soon' },
  ],
  registryStatus: 'success' as 'success' | 'pending' | 'error',
  integrationsByBusiness: {
    'org-a': [
      { platform: 'telegram', status: 'active' },
      { platform: 'vk', status: 'active' },
      { platform: 'yandex_business', status: 'active' },
      { platform: 'google_business', status: 'active' },
    ],
    'org-b': [{ platform: 'vk', status: 'active' }],
  } as Record<string, { platform: string; status: string }[]>,
  get: vi.fn(),
}));

vi.mock('@/lib/hooks/usePlatforms', () => ({
  usePlatforms: () => ({
    platforms: state.registry,
    isSuccess: state.registryStatus === 'success',
    isPending: state.registryStatus === 'pending',
    isError: state.registryStatus === 'error',
  }),
}));
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (businessId: string) => ({ get: () => state.get(businessId) }),
}));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));

function setup(onCompose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return {
    client,
    onCompose,
    ...render(<GuidedCompose onCompose={onCompose} />, { wrapper: Wrapper }),
  };
}

async function openCompose() {
  await userEvent.click(screen.getByRole('button', { name: 'Составить пост' }));
}

beforeEach(() => {
  vi.clearAllMocks();
  state.registryStatus = 'success';
  state.integrationsByBusiness = {
    'org-a': [
      { platform: 'telegram', status: 'active' },
      { platform: 'vk', status: 'active' },
      { platform: 'yandex_business', status: 'active' },
      { platform: 'google_business', status: 'active' },
    ],
    'org-b': [{ platform: 'vk', status: 'active' }],
  };
  state.get.mockImplementation(async (businessId: string) => ({
    data: state.integrationsByBusiness[businessId] ?? [],
  }));
  useBusinessStore.setState({ activeBusinessId: 'org-a' });
});

describe('GuidedCompose connected channel selection', () => {
  it('narrows the seeded instruction to the selected confirmed channel', async () => {
    const { onCompose } = setup();
    await openCompose();
    await waitFor(() => expect(screen.getByLabelText('Telegram')).toBeChecked());
    expect(screen.queryByLabelText('Google Business')).not.toBeInTheDocument();
    await userEvent.click(screen.getByLabelText('VK'));
    await userEvent.click(screen.getByLabelText('Яндекс Бизнес'));
    await userEvent.type(screen.getByLabelText('О чём пост'), 'скидка в выходные');
    await userEvent.click(screen.getByRole('button', { name: 'Подготовить в чате' }));

    const instruction = onCompose.mock.calls[0]?.[0] as string;
    expect(instruction).toContain('Telegram (telegram)');
    expect(instruction).not.toMatch(/VK \(vk\)|yandex_business|google_business/);
    expect(instruction).toContain('Не публикуй в других активных каналах');
  });

  it('does not compose when no channel is selected', async () => {
    const { onCompose } = setup();
    await openCompose();
    await waitFor(() => expect(screen.getByLabelText('Telegram')).toBeChecked());
    for (const name of ['Telegram', 'VK', 'Яндекс Бизнес']) {
      await userEvent.click(screen.getByLabelText(name));
    }
    await userEvent.type(screen.getByLabelText('О чём пост'), 'новость');
    expect(screen.getByRole('button', { name: 'Подготовить в чате' })).toBeDisabled();
    expect(onCompose).not.toHaveBeenCalled();
  });

  it('waits for successful evidence and excludes broken, pending, unavailable and Google rows', async () => {
    let finish!: (value: { data: { platform: string; status: string }[] }) => void;
    state.get.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      })
    );
    setup();
    await openCompose();
    expect(screen.getByText('Проверяем подключённые каналы…')).toBeInTheDocument();
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
    await act(async () => {
      finish({
        data: [
          { platform: 'telegram', status: 'active' },
          { platform: 'vk', status: 'token_expired' },
          { platform: 'yandex_business', status: 'pending' },
          { platform: 'google_business', status: 'active' },
        ],
      });
    });
    await waitFor(() => expect(screen.getByLabelText('Telegram')).toBeChecked());
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
  });

  it('removes a selected channel when fresh integration evidence removes it', async () => {
    const { client, onCompose } = setup();
    await openCompose();
    await waitFor(() => expect(screen.getByLabelText('VK')).toBeChecked());
    state.integrationsByBusiness['org-a'] = [{ platform: 'telegram', status: 'active' }];
    await act(async () => {
      await client.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS('org-a') });
    });
    await waitFor(() => expect(screen.queryByLabelText('VK')).not.toBeInTheDocument());
    await userEvent.type(screen.getByLabelText('О чём пост'), 'обновление');
    await userEvent.click(screen.getByRole('button', { name: 'Подготовить в чате' }));
    expect(onCompose.mock.calls[0]?.[0]).toContain('Telegram (telegram)');
    expect(onCompose.mock.calls[0]?.[0]).not.toContain('VK (vk)');
  });

  it('resets form content and selection when the organization changes', async () => {
    setup();
    await openCompose();
    await waitFor(() => expect(screen.getByLabelText('Telegram')).toBeChecked());
    await userEvent.type(screen.getByLabelText('О чём пост'), 'org-a private topic');
    act(() => useBusinessStore.setState({ activeBusinessId: 'org-b' }));
    await waitFor(() => expect(screen.getByLabelText('VK')).toBeChecked());
    expect(screen.queryByLabelText('Telegram')).not.toBeInTheDocument();
    expect(screen.getByLabelText('О чём пост')).toHaveValue('');
  });
});
