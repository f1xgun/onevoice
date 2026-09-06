import { describe, expect, it } from 'vitest';
import postcss from 'postcss';
import { compileLayoutCSS, hasLayoutBrowser, withLayoutPage } from './browser-layout';

function layoutFixture(): HTMLDivElement {
  const root = document.createElement('div');
  root.className = '[&>button:first-child]:w-8 [&>button:first-child]:h-8';
  root.innerHTML = '<button>Close</button><section><svg class="w-4 h-4"></svg></section>';
  root.querySelector('section')!.className = '[&>svg]:w-8';
  return root;
}

describe('layout CSS compilation', () => {
  it('preserves arbitrary variants on roots and descendants through HTML serialization', async () => {
    const html = layoutFixture().outerHTML;
    expect(html).toContain('&amp;');
    const css = postcss.parse(await compileLayoutCSS(html));
    const declarations: Record<string, Record<string, string>> = {};
    css.walkRules((rule) => {
      declarations[rule.selector] = {};
      rule.walkDecls((declaration) => {
        declarations[rule.selector][declaration.prop] = declaration.value;
      });
    });

    expect(declarations[String.raw`.\[\&\>button\:first-child\]\:w-8>button:first-child`]).toEqual({
      width: '2rem',
    });
    expect(declarations[String.raw`.\[\&\>button\:first-child\]\:h-8>button:first-child`]).toEqual({
      height: '2rem',
    });
    expect(declarations[String.raw`.\[\&\>svg\]\:w-8>svg`]).toEqual({ width: '2rem' });
    expect(declarations['.h-4']).toEqual({ height: '1rem' });
  });

  it.skipIf(!hasLayoutBrowser)('applies arbitrary variants to browser measurements', async () => {
    await withLayoutPage(layoutFixture().outerHTML, { width: 375, height: 667 }, async (page) => {
      const button = await page.getByRole('button', { name: 'Close' }).boundingBox();
      expect(button).toMatchObject({ width: 32, height: 32 });
      const svg = await page.locator('svg').boundingBox();
      expect(svg).toMatchObject({ width: 32, height: 16 });
    });
  });
});
