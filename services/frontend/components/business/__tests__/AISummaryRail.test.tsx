import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';
import { expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AISummaryRail } from '../AISummaryRail';

it('keeps an unbroken organization name within the shrinking summary column', () => {
  const name = 'Организация'.repeat(40);
  const { container } = render(<AISummaryRail business={{ name }} tones={[]} />);
  expect(screen.getByText(new RegExp(name))).toBeVisible();
  expect(container.querySelector('aside')).toHaveClass(
    'min-w-0',
    '[overflow-wrap:anywhere]',
    'xl:sticky'
  );
});

it.skipIf(!hasLayoutBrowser).each(['ru', 'en'] as const)(
  'measures long %s content across mobile and desktop viewports',
  async (locale) => {
    globalThis.__setTestLocale(locale);
    const name = (locale === 'ru' ? 'Организация' : 'Organization').repeat(40);
    const { container } = render(<AISummaryRail business={{ name, address: name }} tones={[]} />);
    for (const width of [320, 768, 1280]) {
      await withLayoutPage(container.innerHTML, { width, height: 720 }, async (page) => {
        const dimensions = await page.locator('aside').evaluate((element) => ({
          width: element.clientWidth,
          scrollWidth: element.scrollWidth,
          right: element.getBoundingClientRect().right,
        }));
        expect(dimensions.width).toBeGreaterThan(0);
        expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.width + 1);
        expect(dimensions.right).toBeLessThanOrEqual(width);
      });
    }
  },
  15000
);
