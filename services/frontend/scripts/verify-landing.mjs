import assert from 'node:assert/strict';
import { mkdir, readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const origin = 'http://localhost:3102';
const output = '.next/landing-check';
const browser = await chromium.launch();
await mkdir(output, { recursive: true });
const results = [];
try {
  for (const locale of ['ru', 'en']) {
    const copy = JSON.parse(await readFile(`messages/${locale}.json`, 'utf8')).landing;
    for (const theme of ['light', 'dark']) {
      for (const width of [320, 375, 1440]) {
        const context = await browser.newContext({
          viewport: { width, height: 812 },
          reducedMotion: 'reduce',
        });
        await context.addCookies([
          { name: 'NEXT_LOCALE', value: locale, url: origin },
          { name: 'NEXT_THEME', value: theme, url: origin },
        ]);
        const page = await context.newPage();
        const externalFonts = [];
        page.on('request', (request) => {
          if (request.resourceType() === 'font' && !request.url().startsWith(origin))
            externalFonts.push(request.url());
        });
        await page.route('**/api/v1/**', (route) => {
          if (route.request().url().endsWith('/platforms'))
            return route.fulfill({
              json: [
                { id: 'telegram', status: 'active' },
                { id: 'vk', status: 'active' },
                { id: 'yandex_business', status: 'active' },
                { id: 'google_business', status: 'coming_soon' },
              ],
            });
          return route.fulfill({ status: 429, json: {} });
        });
        await page.goto(origin, { waitUntil: 'networkidle' });
        await page.evaluate(() => document.fonts.ready);
        assert.equal(await page.locator('h1').innerText(), copy.hero.headline);
        assert.equal(
          await page.locator('#hero [data-cta="hero-waitlist"]').getAttribute('href'),
          '#waitlist'
        );
        assert.equal(
          await page.locator('#hero [data-cta="hero-register"]').getAttribute('href'),
          '/register'
        );
        assert.equal(await page.getByText(copy.hero.betaNote, { exact: true }).isVisible(), true);
        assert.equal(
          await page
            .locator('#work-example button, #work-example a, #work-example [aria-hidden="true"] p')
            .count(),
          0
        );
        assert.equal(
          await page.getByText(copy.workExample.draft, { exact: true }).isVisible(),
          true
        );
        assert.deepEqual(externalFonts, []);
        const geometry = await page.evaluate(() => ({
          width: document.documentElement.scrollWidth,
          exampleTop: document.querySelector('#work-example-title').getBoundingClientRect().top,
          requestTop: document.querySelector('#work-example dd').getBoundingClientRect().top,
          choices: [
            ...document.querySelectorAll(
              '#waitlist label[for^="pain-"], #waitlist label[for="waitlist-consent"]'
            ),
          ].map((element) => element.getBoundingClientRect().height),
        }));
        assert.equal(geometry.width, width, JSON.stringify({ locale, theme, width, geometry }));
        assert(
          geometry.choices.every((height) => height >= 44),
          JSON.stringify(geometry)
        );
        if (width === 375)
          assert(geometry.requestTop + 25 <= 812, JSON.stringify({ locale, theme, geometry }));
        const disabled = page.locator('#waitlist button[type="submit"]');
        assert.equal(await disabled.isDisabled(), true);
        const disabledStyle = await disabled.evaluate((element) => ({
          bg: getComputedStyle(element).backgroundColor,
          fg: getComputedStyle(element).color,
          opacity: getComputedStyle(element).opacity,
        }));
        assert.deepEqual(
          disabledStyle,
          theme === 'light'
            ? { bg: 'rgb(231, 233, 227)', fg: 'rgb(88, 99, 93)', opacity: '1' }
            : { bg: 'rgb(53, 67, 59)', fg: 'rgb(185, 195, 186)', opacity: '1' }
        );
        const cta = page.locator('#hero [data-cta="hero-waitlist"]');
        await cta.hover();
        const hover = await cta.evaluate((element) => ({
          bg: getComputedStyle(element).backgroundColor,
          fg: getComputedStyle(element).color,
        }));
        assert.deepEqual(
          hover,
          theme === 'light'
            ? { bg: 'rgb(25, 70, 63)', fg: 'rgb(255, 255, 255)' }
            : { bg: 'rgb(179, 217, 207)', fg: 'rgb(32, 39, 36)' }
        );
        await page.mouse.move(0, 0);
        await page.screenshot({ path: `${output}/${locale}-${theme}-${width}.png` });
        if (width < 768) {
          await page.locator('#features').scrollIntoViewIfNeeded();
          await page.waitForFunction(() => document.querySelector('[data-landing-bar]'));
          await page.getByLabel(copy.waitlist.emailLabel, { exact: true }).focus();
          await page.waitForFunction(() => !document.querySelector('[data-landing-bar]'));
          await page.getByLabel(copy.waitlist.emailLabel, { exact: true }).fill('invalid');
          assert.equal(await page.locator('#waitlist-email').getAttribute('aria-invalid'), 'true');
          await page.locator('#waitlist-email').fill('owner@example.com');
          await page.locator('label[for="pain-posts"]').click();
          assert.equal(await page.locator('#pain-posts').getAttribute('aria-checked'), 'true');
          await page.locator('label[for="waitlist-consent"]').click({ position: { x: 10, y: 10 } });
          assert.equal(
            await page.locator('#waitlist-consent').getAttribute('aria-checked'),
            'true'
          );
          await page.getByRole('button', { name: copy.waitlist.submit, exact: true }).click();
          await page.getByRole('alert').filter({ hasText: copy.waitlist.errorRateLimit }).waitFor();
          assert.equal(await page.locator('[data-landing-bar]').count(), 0);
          await page.locator('#waitlist a[href="/legal/privacy"]').focus();
          assert.equal(await page.locator('[data-landing-bar]').count(), 0);
        }
        await page.evaluate(() => {
          document.documentElement.style.fontSize = '200%';
        });
        const expanded = await page.evaluate(() => ({
          width: document.documentElement.scrollWidth,
          viewport: window.innerWidth,
        }));
        assert.equal(
          expanded.width,
          expanded.viewport,
          `200% text: ${JSON.stringify({ locale, theme, width, expanded })}`
        );
        await page.evaluate(() => {
          document.documentElement.style.fontSize = '';
          const style = document.createElement('style');
          style.textContent =
            'p, a, label, h1, h2, h3, dd, dt, li { line-height: 1.5 !important; letter-spacing: .12em !important; word-spacing: .16em !important } p { margin-bottom: 2em !important }';
          document.head.append(style);
        });
        assert.equal(
          await page.evaluate(() => document.documentElement.scrollWidth),
          width,
          'Text spacing overflow'
        );
        results.push({ locale, theme, width, ...geometry, hover });
        await context.close();
      }
    }
  }
  console.log(JSON.stringify(results, null, 2));
} finally {
  await browser.close();
}
