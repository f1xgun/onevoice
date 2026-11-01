import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { renderToStaticMarkup } from 'react-dom/server';
import postcss from 'postcss';
import type { Root } from 'postcss';
import tailwindcss from 'tailwindcss';
import { beforeAll, describe, expect, it } from 'vitest';
import config from '@/tailwind.config';
import { ActionButton } from './ActionButton';
import type { ActionButtonProps } from './ActionButton';
import { AppInput } from './AppInput';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

let css: Root;
const variants: ActionButtonProps['variant'][] = [
  'primary',
  'default',
  'accent',
  'secondary',
  'outline',
  'ghost',
  'danger',
  'destructive',
  'link',
];
const hiddenButton = renderToStaticMarkup(
  <ActionButton
    variant="ghost"
    size="icon"
    className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
  >
    Меню
  </ActionButton>
);
const hoverButton = renderToStaticMarkup(
  <ActionButton className="opacity-50 hover:opacity-75">Действие</ActionButton>
);
const markup =
  renderToStaticMarkup(<ActionButton size="sm">Выбрать</ActionButton>) +
  hiddenButton +
  hoverButton +
  variants
    .map((variant) =>
      renderToStaticMarkup(<ActionButton variant={variant}>Ёё Йй Щщ ₽ №</ActionButton>)
    )
    .join('') +
  renderToStaticMarkup(<AppInput />);

beforeAll(async () => {
  css = (
    await postcss([
      tailwindcss({
        ...config,
        content: [{ raw: markup + '<div class="dark ov-app"></div>', extension: 'html' }],
      }),
    ]).process(readFileSync(resolve('app/globals.css'), 'utf8'), { from: undefined })
  ).root;
});

function declarations(element: Element, theme: 'light' | 'dark', state = '', app = true) {
  const values: Record<string, string> = {};
  const weights: Record<string, number> = {};
  css.walkRules((rule) => {
    if (rule.parent?.type === 'atrule' && rule.parent.name === 'media') return;
    if (!app && rule.selector.includes(':has(.ov-app)')) return;
    if (
      rule.selectors.includes(':root') ||
      rule.selectors.includes(':root:has(.ov-app)') ||
      (theme === 'dark' && rule.selectors.includes(':root.dark:has(.ov-app)')) ||
      (theme === 'dark' && rule.selectors.includes('.dark'))
    ) {
      rule.walkDecls((decl) => {
        if (decl.prop.startsWith('--')) values[decl.prop] = decl.value;
      });
    }
  });
  element.setAttribute('data-test-state', state);
  css.walkRules((rule) => {
    if (rule.parent?.type === 'atrule') return;
    let weight = -1;
    for (const raw of rule.selectors) {
      const selector = raw.replace(
        /(?<!\\):(hover|active|focus-visible)/g,
        '[data-test-state~="$1"]'
      );
      try {
        if (element.matches(selector))
          weight = Math.max(weight, (selector.replace(/\\./g, '').match(/\.|\[|:/g) ?? []).length);
      } catch {
        continue;
      }
    }
    if (weight < 0) return;
    rule.walkDecls((decl) => {
      if (weight >= (weights[decl.prop] ?? -1)) {
        values[decl.prop] = decl.value;
        weights[decl.prop] = weight;
      }
    });
  });
  function resolveValue(value: string): string {
    return value.replace(/var\((--[\w-]+)\)/g, (_, token: string) =>
      resolveValue(values[token] ?? token)
    );
  }
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      resolveValue(value).replace(/#[0-9a-f]{6}/gi, (color) => color.toUpperCase()),
    ])
  );
}

function element(html: string) {
  const host = document.createElement('div');
  host.innerHTML = html;
  return host.firstElementChild!;
}

describe('compiled Tailwind button and field cascade', () => {
  it.skipIf(!hasLayoutBrowser).each([375, 1280])(
    'applies v2 and legacy themes at %i px including portals',
    async (width) => {
      const controls = renderToStaticMarkup(
        <>
          <ActionButton>Продолжить</ActionButton>
          <ActionButton size="sm">Выбрать</ActionButton>
          <AppInput />
        </>
      );
      await withLayoutPage(
        `<main class="ov-app">${controls}</main><aside class="bg-card">Portal</aside>`,
        { width, height: 800 },
        async (page) => {
          for (const [theme, scheme, dark] of [
            ['light', 'dark', false],
            ['dark', 'light', true],
            ['', 'dark', true],
            ['', 'light', false],
          ] as const) {
            await page.emulateMedia({ colorScheme: scheme });
            await page.locator('html').evaluate((html, value) => {
              html.className = value;
            }, theme);
            expect(
              await page.locator('body').evaluate((body) => getComputedStyle(body).backgroundColor)
            ).toBe(dark ? 'rgb(20, 22, 25)' : 'rgb(242, 243, 245)');
            expect(
              await page
                .locator('aside')
                .evaluate((aside) => getComputedStyle(aside).backgroundColor)
            ).toBe(dark ? 'rgb(31, 34, 38)' : 'rgb(255, 255, 255)');
            expect(
              await page
                .locator('html')
                .evaluate((html) => getComputedStyle(html).getPropertyValue('--ov-overlay').trim())
            ).toBe(dark ? 'rgb(0 0 0 / 0.6)' : 'rgb(0 0 0 / 0.45)');
            expect((await page.getByRole('button').first().boundingBox())!.height).toBe(48);
            expect((await page.getByRole('button').nth(1).boundingBox())!.height).toBe(40);
          }
          await page.emulateMedia({ colorScheme: 'dark' });
          await page.locator('main').evaluate((main) => main.classList.remove('ov-app'));
          expect(
            await page.locator('body').evaluate((body) => getComputedStyle(body).backgroundColor)
          ).toBe('rgb(23, 25, 29)');
        }
      );
    },
    15000
  );

  it.each(['light', 'dark'] as const)('keeps the %s landing palette and sizing', (theme) => {
    const button = element(renderToStaticMarkup(<ActionButton>Продолжить</ActionButton>));
    const values = declarations(button, theme, '', false);
    expect(values['min-height']).toBe('44px');
    expect(values['--ov-overlay']).toBe(
      theme === 'light' ? 'rgb(0 0 0 / 0.48)' : 'rgb(0 0 0 / 0.64)'
    );
    expect(values['background-color']).toBe(theme === 'light' ? '#245C55' : '#99C6BA');
    expect(values.color).toBe(theme === 'light' ? '#FFFFFF' : '#17191D');
  });

  it('uses compact controls with a larger tap area and v2 surface tokens', () => {
    const button = element(renderToStaticMarkup(<ActionButton size="sm">Выбрать</ActionButton>));
    const values = declarations(button, 'dark');
    expect(values['min-height']).toBe('40px');
    expect(values['--ov-on-count']).toBe('#FFFFFF');
    expect(values['--ov-segment-on']).toBe('#3A3F46');
    expect(values['--ov-radius-lg']).toBe('16px');
    expect(button.className).toContain('after:-inset-y-0.5');
  });

  it('preserves caller visibility in compiled CSS until group hover or keyboard focus', () => {
    const group = element(`<div class="group">${hiddenButton}</div>`);
    const button = group.firstElementChild!;
    expect(declarations(button, 'light').opacity).toBe('0');
    group.setAttribute('data-test-state', 'hover');
    expect(declarations(button, 'light').opacity).toBe('1');
    group.removeAttribute('data-test-state');
    expect(declarations(button, 'light', 'focus-visible').opacity).toBe('1');
    expect(declarations(button, 'light').opacity).toBe('0');
  });

  it('preserves caller opacity overrides for normal and hover states', () => {
    const button = element(hoverButton);
    expect(declarations(button, 'light').opacity).toBe('0.5');
    expect(declarations(button, 'light', 'hover').opacity).toBe('0.75');
  });

  it.each(['light', 'dark'] as const)(
    '%s resolves actual generated primary/hover/disabled pairs',
    (theme) => {
      for (const variant of variants) {
        const button = element(
          renderToStaticMarkup(<ActionButton variant={variant}>Продолжить</ActionButton>)
        );
        const normal = declarations(button, theme);
        expect(normal['--ov-overlay']).toBe(
          theme === 'light' ? 'rgb(0 0 0 / 0.45)' : 'rgb(0 0 0 / 0.6)'
        );
        const hover = declarations(button, theme, 'hover');
        const primary = ['primary', 'default', 'accent'].includes(variant!);
        const dangerous = ['danger', 'destructive'].includes(variant!);
        expect(normal['min-height']).toBe('48px');
        expect(normal['font-size']).toBe('1rem');
        expect(normal['white-space']).toBe('normal');
        expect(normal['opacity']).toBe('1');
        if (primary) {
          expect(normal['background-color']).toBe(theme === 'light' ? '#245C55' : '#8FC2B4');
          expect(hover['background-color']).toBe(theme === 'light' ? '#19463F' : '#ACD6CB');
          expect(hover.color).toBe(theme === 'light' ? '#FFFFFF' : '#141619');
        } else if (variant !== 'link') {
          expect(normal['background-color']).toBe(theme === 'light' ? '#E8EAED' : '#2C3035');
          expect(hover['background-color']).toBe(theme === 'light' ? '#DDE0E4' : '#363B41');
          expect(hover.color).toBe(
            dangerous
              ? theme === 'light'
                ? '#B4362E'
                : '#F0A097'
              : theme === 'light'
                ? '#1F2422'
                : '#ECEEF0'
          );
        } else {
          expect(normal['text-decoration-line']).toBe('underline');
        }
        button.setAttribute('disabled', '');
        const disabled = declarations(button, theme, 'hover');
        expect(disabled['background-color']).toBe(theme === 'light' ? '#F2F3F5' : '#181A1D');
        expect(disabled.color).toBe(theme === 'light' ? '#5B6360' : '#A2A9B1');
        expect(disabled['border-color']).toBe(theme === 'light' ? '#C9CED3' : '#5D646D');
        expect(disabled.opacity).toBe('1');
      }
    }
  );

  it.each(['light', 'dark'] as const)(
    '%s fields compile control borders and visible focus',
    (theme) => {
      const input = element(renderToStaticMarkup(<AppInput />));
      expect(declarations(input, theme)['border-color']).toBe(
        theme === 'light' ? '#C9CED3' : '#5D646D'
      );
      const focused = declarations(input, theme, 'focus-visible');
      expect(focused['--tw-ring-color']).toBe(theme === 'light' ? '#245C55' : '#8FC2B4');
      expect(focused['--tw-ring-offset-width']).toBe('2px');
    }
  );
});
