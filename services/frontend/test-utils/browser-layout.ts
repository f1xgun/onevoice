import { existsSync } from 'node:fs';
import { chromium } from 'playwright';
import type { Page } from 'playwright';
import postcss from 'postcss';
import tailwindcss from 'tailwindcss';
import tailwindConfig from '../tailwind.config';

export const hasLayoutBrowser =
  process.env.RUN_LAYOUT_TESTS === '1' && existsSync(chromium.executablePath());

export async function compileLayoutCSS(html: string): Promise<string> {
  const document = new DOMParser().parseFromString(html, 'text/html');
  const classes = Array.from(document.querySelectorAll('[class]')).flatMap((element) =>
    Array.from(element.classList)
  );
  const css = await postcss([
    tailwindcss({
      ...tailwindConfig,
      content: [{ raw: [...new Set(classes)].join('\n'), extension: 'html' }],
    }),
  ]).process('@tailwind base; @tailwind utilities;', { from: undefined });
  return css.css;
}

export async function withLayoutPage(
  html: string,
  viewport: { width: number; height: number },
  check: (page: Page) => Promise<void>
): Promise<void> {
  const css = await compileLayoutCSS(html);
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport });
    await page.setContent(html);
    await page.addStyleTag({ content: css });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.addStyleTag({
      content: '* { animation: none !important; transition: none !important; }',
    });
    await check(page);
  } finally {
    await browser.close();
  }
}
