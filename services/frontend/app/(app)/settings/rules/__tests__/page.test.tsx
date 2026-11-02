import { mkdirSync } from 'node:fs';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import RulesPage from '../page';

const get = vi.fn();
const put = vi.fn();
const permission = { allowed: true, isLoading: false, isError: false, refetch: vi.fn() };
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: 'business-a' }),
}));
vi.mock('@/lib/api/business-api', () => ({ bizApi: () => ({ get, put }) }));
vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => permission }));
vi.mock('@/lib/hooks/useTools', () => ({
  useTools: () => ({ data: [], isLoading: false, isError: false }),
}));
vi.mock('@/lib/hooks/useBusinessToolApprovals', () => ({
  useBusinessToolApprovals: () => ({ data: {}, isLoading: false, isError: false }),
}));
vi.mock('@/components/business/VoiceToneSection', () => ({ VoiceToneSection: () => null }));
vi.mock('@/components/business/VoiceProfileSection', () => ({
  VoiceProfileSection: () => <div data-testid="voice-profile-editor" />,
}));
beforeEach(() => {
  get.mockReset();
  put.mockReset();
  Object.assign(permission, { allowed: true, isLoading: false, isError: false });
});
function mount(config: object, settings: object = {}) {
  get.mockImplementation((path: string) =>
    Promise.resolve({
      data: path === '/review-autopilot' ? config : { name: 'Point', settings },
    })
  );
  put.mockResolvedValue({});
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <RulesPage />
    </QueryClientProvider>
  );
}
it.each([undefined, '', ' \n  '])(
  'shows an editable empty profile for %j',
  async (voiceProfile) => {
    mount(
      { enabled: true, minRating: 5, ignoreFourStar: true, autoAnswerFacts: false },
      { voiceProfile }
    );
    const profile = await screen.findByRole('button', {
      name: 'О точке своими словами: Не заполнено',
    });
    expect(screen.getByText('Не заполнено')).toHaveClass('text-ink-soft');
    fireEvent.click(profile);
    expect(screen.getByTestId('voice-profile-editor')).toBeInTheDocument();
  }
);
it('shows the saved business description instead of the empty placeholder', async () => {
  mount(
    { enabled: true, minRating: 5, ignoreFourStar: true, autoAnswerFacts: false },
    { voiceProfile: 'Обжариваем зерно сами' }
  );
  expect(
    await screen.findByRole('button', { name: 'О точке своими словами: Обжариваем зерно сами' })
  ).toBeInTheDocument();
  expect(screen.queryByText('Не заполнено')).toBeNull();
});
it('shows saved four-star auto instead of replacing it with the new default', async () => {
  mount({ enabled: true, minRating: 4, ignoreFourStar: false, autoAnswerFacts: false });
  expect(await screen.findByRole('button', { name: 'На 4 звезды: Отвечает сам' })).toBeEnabled();
});
it('saves four-star ignore through the real API and keeps five-star auto', async () => {
  mount({ enabled: true, minRating: 4, ignoreFourStar: false, autoAnswerFacts: false });
  fireEvent.click(await screen.findByRole('button', { name: 'На 4 звезды: Отвечает сам' }));
  fireEvent.click(screen.getByRole('button', { name: 'Не отвечает' }));
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/review-autopilot', {
      enabled: true,
      minRating: 5,
      ignoreFourStar: true,
      autoAnswerFacts: false,
    })
  );
});
it('keeps editing unavailable while permissions fail to load', async () => {
  Object.assign(permission, { allowed: false, isError: true });
  mount({ enabled: false, minRating: 5, ignoreFourStar: true, autoAnswerFacts: false });
  expect(await screen.findByRole('button', { name: 'На 4 звезды: Не отвечает' })).toBeDisabled();
  expect(screen.getByRole('button', { name: /Повторить/ })).toBeInTheDocument();
});

it.skipIf(!hasLayoutBrowser)(
  'keeps rules and editor inside phone and desktop widths in both themes',
  async () => {
    mount({ enabled: true, minRating: 5, ignoreFourStar: true, autoAnswerFacts: false });
    const four = await screen.findByRole('button', { name: 'На 4 звезды: Не отвечает' });
    mkdirSync('/private/tmp/onevoice-redesign/rules', { recursive: true });
    for (const scene of ['page', 'sheet']) {
      if (scene === 'sheet') fireEvent.click(four);
      for (const width of [375, 1280]) {
        await withLayoutPage(
          `<div class="ov-app">${document.body.innerHTML}</div>`,
          { width, height: 900 },
          async (page) => {
            for (const theme of ['light', 'dark']) {
              await page.locator('html').evaluate((html, value) => {
                html.className = value;
              }, theme);
              expect(
                await page.locator('body').evaluate((body) => body.scrollWidth)
              ).toBeLessThanOrEqual(width);
              await page.screenshot({
                path: `/private/tmp/onevoice-redesign/rules/${scene}-${width}-${theme}.png`,
                fullPage: true,
              });
            }
          }
        );
      }
    }
  },
  20000
);

it('edits factual replies independently while preserving rating and ignore settings', async () => {
  mount({ enabled: false, minRating: 5, ignoreFourStar: true, autoAnswerFacts: false });
  fireEvent.click(
    await screen.findByRole('button', { name: 'Адрес и обычные часы: Готовит черновик вам' })
  );
  fireEvent.click(screen.getByRole('button', { name: 'Отвечает сам' }));
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/review-autopilot', {
      enabled: false,
      minRating: 5,
      ignoreFourStar: true,
      autoAnswerFacts: true,
    })
  );
});
