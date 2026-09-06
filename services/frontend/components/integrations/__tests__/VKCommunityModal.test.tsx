import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { VKCommunityModal } from '../VKCommunityModal';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const BUSINESS_ID = 'biz-1';

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

const apiGet = vi.fn();
const apiPost = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: (...args: unknown[]) => apiGet(...args),
    post: (...args: unknown[]) => apiPost(...args),
    put: vi.fn(),
    delete: vi.fn(),
  }),
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

// The paste UI now lives inside a collapsed <details> "or paste a token
// manually" affordance. Expand it before interacting with the textarea.
async function expandPaste(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByText('или вставить ключ вручную'));
}

describe('VKCommunityModal — Authorize with VK (primary path)', () => {
  const originalLocation = window.location;

  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    vi.mocked(toast.success).mockReset();
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

  it('GETs vk.authUrl and redirects window.location.href to the returned url', async () => {
    apiGet.mockResolvedValueOnce({
      data: { url: 'https://oauth.vk.com/authorize?client_id=1&state=abc' },
    });
    const user = userEvent.setup();
    renderModal();

    await user.click(screen.getByRole('button', { name: 'Войти через ВКонтакте' }));

    await waitFor(() => expect(apiGet).toHaveBeenCalledWith('/integrations/vk/auth-url'));
    await waitFor(() =>
      expect(window.location.href).toBe('https://oauth.vk.com/authorize?client_id=1&state=abc')
    );
  });

  it('on auth-url failure toasts vkAuthFailed and keeps the paste fallback reachable', async () => {
    apiGet.mockRejectedValueOnce(new Error('vk oauth unconfigured'));
    const user = userEvent.setup();
    renderModal();

    await user.click(screen.getByRole('button', { name: 'Войти через ВКонтакте' }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Не получилось открыть авторизацию ВКонтакте. Попробуйте вставить ключ вручную.'
      )
    );
    expect(window.location.href).toBe('');
    expect(screen.getByLabelText('Ключ доступа сообщества')).toBeInTheDocument();
  });
});

describe('VKCommunityModal — paste flow (fallback)', () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    vi.mocked(toast.success).mockReset();
    vi.mocked(toast.error).mockReset();
  });

  it('renders the title and the inline instructions', async () => {
    const user = userEvent.setup();
    renderModal();
    expect(screen.getByText('Подключить сообщество ВКонтакте')).toBeInTheDocument();
    await expandPaste(user);
    expect(screen.getByText(/Где взять ключ/)).toBeInTheDocument();
    expect(screen.getByText(/Не выбирайте приложение/)).toBeInTheDocument();
  });

  it('disables Submit when the textarea is empty or whitespace-only', async () => {
    const user = userEvent.setup();
    renderModal();
    await expandPaste(user);
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
    await expandPaste(user);

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
    await expandPaste(user);

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
    await expandPaste(user);

    await user.type(screen.getByLabelText('Ключ доступа сообщества'), 'vk1.a.X');
    await user.click(screen.getByRole('button', { name: /^Подключить$/ }));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Не удалось подключить сообщество')
    );
  });
});

it('keeps expanded token content in an internal scroller with reachable dismissal', async () => {
  const user = userEvent.setup();
  const { onClose } = renderModal();
  await expandPaste(user);
  const dialog = screen.getByRole('dialog');
  expect(dialog).toHaveClass('max-h-[calc(100dvh-2rem)]', 'overflow-hidden');
  const details = dialog.querySelector('details')!;
  expect(details).toHaveAttribute('open');
  expect(details.parentElement?.parentElement).toHaveClass('min-h-0', 'overflow-y-auto');
  const cancel = screen.getByRole('button', { name: 'Отмена' });
  expect(details.parentElement).not.toContainElement(cancel);
  expect(screen.getByRole('button', { name: 'Закрыть', exact: true })).toHaveAccessibleName();
  await user.click(cancel);
  expect(onClose).toHaveBeenCalledOnce();
});

it.skipIf(!hasLayoutBrowser).each(['ru', 'en'] as const)(
  'keeps the expanded %s token form reachable when the keyboard shrinks the viewport',
  async (locale) => {
    globalThis.__setTestLocale(locale);
    renderModal();
    const details = screen.getByRole('dialog').querySelector('details')!;
    await userEvent.setup().click(details.querySelector('summary')!);
    const dialog = screen.getByRole('dialog');
    await withLayoutPage(dialog.outerHTML, { width: 375, height: 667 }, async (page) => {
      await page.locator('textarea').focus();
      await page.setViewportSize({ width: 375, height: 360 });
      const box = await page.getByRole('dialog').boundingBox();
      expect(box!.y).toBeGreaterThanOrEqual(0);
      expect(box!.y + box!.height).toBeLessThanOrEqual(360);
      const scroller = page.locator('details').locator('../..');
      const sizes = await scroller.evaluate((element) => ({
        height: element.clientHeight,
        scrollHeight: element.scrollHeight,
      }));
      expect(sizes.scrollHeight).toBeGreaterThan(sizes.height);
      await page.locator('textarea').scrollIntoViewIfNeeded();
      const input = await page.locator('textarea').boundingBox();
      expect(input!.y).toBeGreaterThanOrEqual(box!.y);
      expect(input!.y + input!.height).toBeLessThanOrEqual(360);
      const cancel = page.getByRole('button', {
        name: locale === 'ru' ? 'Отмена' : 'Cancel',
        exact: true,
      });
      const cancelBox = await cancel.boundingBox();
      expect(cancelBox!.y).toBeGreaterThanOrEqual(0);
      expect(cancelBox!.y + cancelBox!.height).toBeLessThanOrEqual(360);
    });
  },
  15000
);
