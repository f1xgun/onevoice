import { existsSync, readFileSync, readdirSync } from 'node:fs';
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
      content: [{ raw: [...new Set([...classes, 'light', 'dark'])].join('\n'), extension: 'html' }],
    }),
  ]).process(readFileSync('app/globals.css', 'utf8'), { from: undefined });
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
    if (existsSync('.next/static/css')) {
      const styles = readdirSync('.next/static/css')
        .filter((file) => file.endsWith('.css'))
        .map((file) => readFileSync(`.next/static/css/${file}`, 'utf8'))
        .join('');
      const fonts = (styles.match(/@font-face\{[^}]+\}/g) ?? []).join('');
      await page.addStyleTag({
        content: fonts.replace(
          /url\(\/_next\/static\/media\/([^)]*)\)/g,
          (_match, file: string) =>
            `url(data:font/woff2;base64,${readFileSync(`.next/static/media/${file}`).toString('base64')})`
        ),
      });
    }
    await page.addStyleTag({
      content:
        ':root { --font-sans: "Golos Text", Arial; --font-mono: "JetBrains Mono", monospace; }',
    });
    await page.evaluate(() => document.fonts.ready);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.addStyleTag({
      content: '* { animation: none !important; transition: none !important; }',
    });
    await check(page);
  } finally {
    await browser.close();
  }
}
