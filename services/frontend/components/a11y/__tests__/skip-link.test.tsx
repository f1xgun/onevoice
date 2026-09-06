import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SkipLink } from '../skip-link';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

describe('skip link geometry and keyboard access', () => {
  it.skipIf(!hasLayoutBrowser)(
    'fits a 320px viewport before and after keyboard focus',
    async () => {
      const { container } = render(
        <>
          <SkipLink />
          <main id="main-content" tabIndex={-1}>
            Content
          </main>
        </>
      );
      await withLayoutPage(container.innerHTML, { width: 320, height: 640 }, async (page) => {
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
        await page.keyboard.press('Tab');
        const link = page.getByRole('link');
        const bounds = await link.boundingBox();
        expect(bounds!.x).toBeGreaterThanOrEqual(0);
        expect(bounds!.width).toBeGreaterThan(1);
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
        await page.keyboard.press('Enter');
        expect(await page.evaluate(() => document.activeElement?.id)).toBe('main-content');
      });
    }
  );
});
