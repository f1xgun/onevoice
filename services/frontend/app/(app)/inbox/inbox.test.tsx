import { mkdirSync, readFileSync } from 'node:fs';
import { beforeEach, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useBusinessStore } from '@/lib/stores/business';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import InboxPage from './page';

const state = vi.hoisted(() => ({
  canWrite: true,
  empty: false,
  error: false,
  failed: false,
  one: false,
  capped: false,
  permissionLoading: false,
  permissionError: false,
  comment: false,
}));
const apiPut = vi.hoisted(() => vi.fn().mockResolvedValue({}));
const apiPost = vi.hoisted(() => vi.fn().mockResolvedValue({}));
vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({
    allowed: state.canWrite,
    isLoading: state.permissionLoading,
    isError: state.permissionError,
    refetch: vi.fn(),
  }),
}));
vi.mock('@/hooks/useOnboardingProgress', () => ({
  useOnboardingProgress: () => ({ loaded: true, allDone: true }),
}));
vi.mock('@/hooks/useDecisionQueue', async (original) => {
  const actual = await original<Record<string, unknown>>();
  return {
    ...actual,
    useDecisionQueue: () => ({
      isError: state.error,
      refetch: vi.fn(),
      data: {
        total: state.empty ? 0 : state.one ? 1 : state.capped ? 101 : 4,
        items: state.empty
          ? []
          : [
              {
                id: 'review:r1',
                kind: 'review',
                platform: 'yandex_business',
                createdAt: '2026-11-01T10:00:00Z',
                review: {
                  id: 'r1',
                  businessId: 'biz-1',
                  platform: state.comment ? 'vk' : 'yandex_business',
                  authorName: 'Ирина Самойлова',
                  rating: state.comment ? 0 : 2,
                  text: 'Заказывала раф с собой, ждала 20 минут.',
                  draftReply: state.failed
                    ? undefined
                    : 'Ирина, спасибо, что написали. Простите за ожидание.',
                  replyText: state.failed ? 'Исправленный ответ' : undefined,
                  draftStatus: 'ready',
                  replyStatus: state.failed ? 'error' : 'pending',
                },
              },
              {
                id: 'approval:b1',
                kind: 'approval_batch',
                conversationId: 'conversation-1',
                callCount: 2,
              },
              { id: 'connection:vk', kind: 'connection', platform: 'vk' },
              {
                id: 'drift:ya',
                kind: 'drift',
                platform: 'yandex_business',
                drift: { driftFields: ['schedule'] },
              },
            ].slice(0, state.one ? 1 : undefined),
        automatic: [
          { id: 'automatic-1', action: 'review.auto_replied', createdAt: '2026-11-01T08:02:00Z' },
        ],
      },
    }),
  };
});
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    put: apiPut,
    post: apiPost,
    get: async () => ({ data: { draftReply: 'Новый вариант ответа' } }),
  }),
}));

beforeEach(() => {
  Object.assign(state, {
    canWrite: true,
    empty: false,
    error: false,
    failed: false,
    one: false,
    capped: false,
    permissionLoading: false,
    permissionError: false,
    comment: false,
  });
  apiPut.mockReset().mockResolvedValue({});
  apiPost.mockReset().mockResolvedValue({});
  useBusinessStore.setState({ activeBusinessId: 'biz-1' });
});
function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <div className="ov-app xl:ml-[232px]">
        <InboxPage />
      </div>
    </QueryClientProvider>
  );
}
it('opens the first review and sends only the explicitly edited text', async () => {
  renderPage();
  await userEvent.click(screen.getByRole('button', { name: 'Изменить черновик' }));
  const field = screen.getByRole('textbox');
  await userEvent.clear(field);
  await userEvent.type(field, 'Исправленный ответ');
  await userEvent.click(screen.getByRole('button', { name: 'Ответить от имени точки' }));
  expect(apiPut).toHaveBeenCalledWith('/reviews/r1/reply', { replyText: 'Исправленный ответ' });
});
it('records a deliberate skip and offers real regeneration', async () => {
  renderPage();
  await userEvent.click(screen.getByRole('button', { name: 'Не отвечать' }));
  expect(apiPost).toHaveBeenCalledWith('/reviews/r1/skip');
  await userEvent.click(screen.getByRole('button', { name: 'Другой вариант' }));
  expect(apiPost).toHaveBeenCalledWith('/reviews/r1/regenerate');
  expect(await screen.findByText('Новый вариант ответа')).toBeInTheDocument();
});
it('opens the whole approval batch without resolving or rejecting sibling calls', async () => {
  renderPage();
  await userEvent.click(screen.getByRole('button', { name: /Поручение ждет решения/ }));
  expect(screen.getByRole('link', { name: 'Открыть поручение и проверить' })).toHaveAttribute(
    'href',
    '/chat/conversation-1'
  );
  expect(apiPost).not.toHaveBeenCalled();
});
it('keeps read-only actors unable to send or suppress a reply', () => {
  state.canWrite = false;
  renderPage();
  expect(screen.queryByRole('textbox')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Изменить черновик' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Не отвечать' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Ответить от имени точки' })).toBeNull();
});
it('keeps errors distinct from a confirmed empty queue', () => {
  state.error = true;
  renderPage();
  expect(screen.getByRole('alert')).toHaveTextContent('Не удалось загрузить очередь');
  expect(screen.queryByText('Сейчас ничего не ждет вашего слова')).toBeNull();
});
it.skipIf(!hasLayoutBrowser).each(['normal', 'empty', 'readonly', 'one', 'failed'] as const)(
  'keeps %s at 375/1280 in both themes',
  async (scenario) => {
    state.empty = scenario === 'empty';
    state.one = scenario === 'one';
    state.failed = scenario === 'failed';
    state.canWrite = scenario !== 'readonly';
    renderPage();
    document.querySelectorAll('textarea').forEach((field) => {
      field.textContent = field.value;
    });
    mkdirSync('/private/tmp/onevoice-redesign/decisions', { recursive: true });
    for (const width of [375, 1280])
      await withLayoutPage(document.body.innerHTML, { width, height: 900 }, async (page) => {
        await page.route('**/platforms/*.svg', (route) => {
          const file = new URL(route.request().url()).pathname.split('/').pop()!;
          return route.fulfill({
            contentType: 'image/svg+xml',
            body: readFileSync(`public/platforms/${file}`, 'utf8'),
          });
        });
        await page.locator('img').evaluateAll((images) =>
          images.forEach((img) => {
            img.setAttribute('src', `http://localhost${img.getAttribute('src')}`);
          })
        );
        for (const theme of ['light', 'dark']) {
          await page.locator('html').evaluate((html, value) => {
            html.setAttribute('class', value);
          }, theme);
          expect(
            await page.locator('body').evaluate((body) => body.scrollWidth)
          ).toBeLessThanOrEqual(width);
          await page.screenshot({
            path: `/private/tmp/onevoice-redesign/decisions/${scenario}-${width}-${theme}.png`,
            fullPage: true,
          });
        }
      });
  },
  20000
);

it('keeps a failed send visible after reload and retries the stored reply only', async () => {
  apiPut.mockRejectedValueOnce(new Error('offline'));
  const first = renderPage();
  await userEvent.click(screen.getByRole('button', { name: 'Изменить черновик' }));
  await userEvent.clear(screen.getByRole('textbox'));
  await userEvent.type(screen.getByRole('textbox'), 'Исправленный ответ');
  await userEvent.click(screen.getByRole('button', { name: 'Ответить от имени точки' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  first.unmount();
  state.failed = true;
  renderPage();
  expect(screen.getByText('Исправленный ответ')).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Другой вариант' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Не отвечать' })).toBeNull();
  await userEvent.click(screen.getByRole('button', { name: 'Отправить еще раз' }));
  expect(apiPost).toHaveBeenCalledWith('/reviews/r1/reply/retry');
  expect(apiPut).toHaveBeenCalledTimes(1);
});
it('does not rate comments and exposes the full review history beyond the cap', () => {
  state.comment = true;
  state.capped = true;
  renderPage();
  expect(screen.queryByText('0 из 5')).toBeNull();
  expect(screen.getByRole('link', { name: 'Все отзывы' })).toHaveAttribute('href', '/reviews');
});
it('hides the empty remaining queue on mobile when only one decision exists', () => {
  state.one = true;
  renderPage();
  expect(screen.getByText('Еще ждут').parentElement).toHaveClass('hidden', 'xl:block');
});
it.each(['loading', 'error'])('does not mislabel permission %s as read-only', (status) => {
  state.permissionLoading = status === 'loading';
  state.permissionError = status === 'error';
  renderPage();
  expect(screen.queryByText('Вы можете читать. Для ответа нужно право публикации.')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Ответить от имени точки' })).toBeNull();
  expect(screen.getByRole(status === 'loading' ? 'status' : 'alert')).toBeInTheDocument();
});
