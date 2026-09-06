import { readFileSync } from 'node:fs';
import postcss from 'postcss';
import type { Node, Root, Rule } from 'postcss';
import tailwindcss from 'tailwindcss';
import { beforeAll, expect, it } from 'vitest';
import config from '@/tailwind.config';

const source = readFileSync('app/globals.css', 'utf8');
let compiled: Root;
beforeAll(async () => {
  compiled = (
    await postcss([
      tailwindcss({
        ...config,
        content: [{ raw: '<div class="dark light dark:underline"></div>' }],
      }),
    ]).process(source, { from: undefined })
  ).root;
});
function values(rule: Rule) {
  const result: Record<string, string> = {};
  rule.walkDecls((decl) => {
    result[decl.prop] = decl.value;
  });
  return result;
}
it('keeps explicit and system dark declarations identical, including newly added tokens', () => {
  const root = postcss.parse(source);
  const explicit: Rule[] = [];
  const system: Rule[] = [];
  root.walkRules((rule) => {
    if (rule.selector === '.dark') explicit.push(rule);
    if (
      rule.selector === ':root:not(.light)' &&
      rule.parent?.type === 'atrule' &&
      rule.parent.params === '(prefers-color-scheme: dark)'
    )
      system.push(rule);
  });
  expect(explicit).toHaveLength(1);
  expect(system).toHaveLength(1);
  expect(values(explicit[0])).toEqual(values(system[0]));
  expect(values(explicit[0])['color-scheme']).toBe('dark');
});
it.each([
  ['system', false, false],
  ['system', true, true],
  ['light', false, false],
  ['light', true, false],
  ['dark', false, true],
  ['dark', true, true],
] as const)(
  '%s with system dark=%s resolves tokens and dark utilities',
  (theme, systemDark, dark) => {
    document.documentElement.className = theme === 'system' ? '' : theme;
    const target = document.createElement('div');
    target.className = 'dark:underline';
    document.body.append(target);
    const rootValues: Record<string, string> = {};
    let underline = false;
    compiled.walkRules((rule) => {
      let parent: Node['parent'] = rule.parent;
      while (parent) {
        if (
          parent.type === 'atrule' &&
          'name' in parent &&
          'params' in parent &&
          parent.name === 'media' &&
          parent.params === '(prefers-color-scheme: dark)' &&
          !systemDark
        )
          return;
        parent = parent.parent;
      }
      if (
        !rule.selector.includes(':root') &&
        !rule.selectors.includes('.dark') &&
        !rule.selector.includes('underline')
      )
        return;
      if (document.documentElement.matches(rule.selector)) Object.assign(rootValues, values(rule));
      if (target.matches(rule.selector) && values(rule)['text-decoration-line'] === 'underline')
        underline = true;
    });
    expect(rootValues['--ov-paper']).toBe(dark ? '#202724' : '#f5f4f0');
    expect(rootValues['--ov-ink']).toBe(dark ? '#f2f3ed' : '#202724');
    expect(rootValues['color-scheme']).toBe(dark ? 'dark' : 'light');
    expect(underline).toBe(dark);
    target.remove();
    document.documentElement.className = '';
  }
);
