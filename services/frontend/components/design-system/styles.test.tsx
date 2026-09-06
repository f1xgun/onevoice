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
const markup =
  variants
    .map((variant) =>
      renderToStaticMarkup(<ActionButton variant={variant}>Ёё Йй Щщ ₽ №</ActionButton>)
    )
    .join('') + renderToStaticMarkup(<AppInput />);

beforeAll(async () => {
  css = (
    await postcss([
      tailwindcss({
        ...config,
        content: [{ raw: markup + '<div class="dark"></div>', extension: 'html' }],
      }),
    ]).process(readFileSync(resolve('app/globals.css'), 'utf8'), { from: undefined })
  ).root;
});

function declarations(element: Element, theme: 'light' | 'dark', state = '') {
  const values: Record<string, string> = {};
  const weights: Record<string, number> = {};
  css.walkRules((rule) => {
    if (
      rule.selectors.includes(':root') ||
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
  it.each(['light', 'dark'] as const)(
    '%s resolves actual generated primary/hover/disabled pairs',
    (theme) => {
      for (const variant of variants) {
        const button = element(
          renderToStaticMarkup(<ActionButton variant={variant}>Продолжить</ActionButton>)
        );
        const normal = declarations(button, theme);
        const hover = declarations(button, theme, 'hover');
        const primary = ['primary', 'default', 'accent'].includes(variant!);
        const dangerous = ['danger', 'destructive'].includes(variant!);
        expect(normal['min-height']).toBe('2.75rem');
        expect(normal['font-size']).toBe('1rem');
        expect(normal['white-space']).toBe('normal');
        expect(normal['opacity']).toBe('1');
        if (primary) {
          expect(normal['background-color']).toBe(theme === 'light' ? '#245C55' : '#99C6BA');
          expect(hover['background-color']).toBe(theme === 'light' ? '#19463F' : '#B3D9CF');
          expect(hover.color).toBe(theme === 'light' ? '#FFFFFF' : '#202724');
        } else if (variant !== 'link') {
          expect(normal['background-color']).toBe(theme === 'light' ? '#FFFFFF' : '#29332E');
          expect(hover['background-color']).toBe(theme === 'light' ? '#E7E9E3' : '#35433B');
          expect(hover.color).toBe(
            dangerous
              ? theme === 'light'
                ? '#A13C36'
                : '#F1A39A'
              : theme === 'light'
                ? '#202724'
                : '#F2F3ED'
          );
        } else {
          expect(normal['text-decoration-line']).toBe('underline');
        }
        button.setAttribute('disabled', '');
        const disabled = declarations(button, theme, 'hover');
        expect(disabled['background-color']).toBe(theme === 'light' ? '#E7E9E3' : '#35433B');
        expect(disabled.color).toBe(theme === 'light' ? '#58635D' : '#B9C3BA');
        expect(disabled['border-color']).toBe(theme === 'light' ? '#77847C' : '#829389');
        expect(disabled.opacity).toBe('1');
      }
    }
  );

  it.each(['light', 'dark'] as const)(
    '%s fields compile control borders and visible focus',
    (theme) => {
      const input = element(renderToStaticMarkup(<AppInput />));
      expect(declarations(input, theme)['border-color']).toBe(
        theme === 'light' ? '#77847C' : '#829389'
      );
      const focused = declarations(input, theme, 'focus-visible');
      expect(focused['--tw-ring-color']).toBe(theme === 'light' ? '#245C55' : '#99C6BA');
      expect(focused['--tw-ring-offset-width']).toBe('2px');
    }
  );
});
