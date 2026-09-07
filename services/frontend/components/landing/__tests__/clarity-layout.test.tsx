import { expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import LandingPage from '@/app/page';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

vi.stubGlobal(
  'IntersectionObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
);

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock('@/components/ui/LanguageSwitcher', () => ({ LanguageSwitcher: () => null }));
vi.mock('@/components/landing/ChannelVote', () => ({ ChannelVote: () => null }));
vi.mock('@/components/landing/SupportedPlatforms', () => ({ SupportedPlatforms: () => null }));

it.skipIf(!hasLayoutBrowser).each(['ru', 'en'] as const)(
  'keeps the example readable in both themes and narrow/wide viewports: %s',
  async (locale) => {
    globalThis.__setTestLocale(locale);
    const { container } = render(<LandingPage />);
    for (const width of [320, 375, 1440]) {
      await withLayoutPage(container.innerHTML, { width, height: 812 }, async (page) => {
        for (const theme of ['light', 'dark']) {
          await page.locator('html').evaluate((element, value) => {
            element.className = value;
          }, theme);
          const example = page.locator('#work-example');
          await expect(example.locator('button').count()).resolves.toBe(0);
          const bounds = await example.evaluate((element) => ({
            width: element.clientWidth,
            scroll: element.scrollWidth,
          }));
          expect(bounds.scroll).toBeLessThanOrEqual(bounds.width + 1);
          if (width === 375) {
            const title = await page.locator('#work-example-title').boundingBox();
            expect(title!.y + title!.height).toBeLessThan(812);
          }
          await example.scrollIntoViewIfNeeded();
          const text = await example
            .locator('dd')
            .first()
            .evaluate((element) => getComputedStyle(element).fontSize);
          expect(parseFloat(text)).toBeGreaterThanOrEqual(16);
        }
      });
    }
  },
  30000
);
