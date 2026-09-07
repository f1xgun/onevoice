import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { HoursForm } from '../ScheduleForm';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

vi.mock('@/lib/hooks/usePermission', () => ({
  usePermission: () => ({ allowed: true, isLoading: false, isError: false }),
}));

function renderHours(locale: 'ru' | 'en') {
  (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
    locale
  );
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <HoursForm />
    </QueryClientProvider>
  );
}

describe.each(['ru', 'en'] as const)('weekly hours in %s', (locale) => {
  it('toggles every day from its label, touch and keyboard without changing other days', async () => {
    renderHours(locale);
    const user = userEvent.setup();
    const switches = screen.getAllByRole('switch');
    expect(switches).toHaveLength(7);
    for (const control of switches) {
      const initial = control.getAttribute('aria-checked');
      const others = switches.filter((other) => other !== control);
      const states = others.map((other) => other.getAttribute('aria-checked'));
      const label = control.closest('label');
      expect(label).not.toBeNull();
      await user.click(label!);
      expect(control).toHaveAttribute('aria-checked', String(initial !== 'true'));
      control.focus();
      await user.keyboard(' ');
      expect(control).toHaveAttribute('aria-checked', initial);
      await user.pointer([
        { keys: '[TouchA>]', target: label! },
        { keys: '[/TouchA]', target: label! },
      ]);
      expect(control).toHaveAttribute('aria-checked', String(initial !== 'true'));
      await user.keyboard('{Enter}');
      expect(control).toHaveAttribute('aria-checked', initial);
      expect(others.map((other) => other.getAttribute('aria-checked'))).toEqual(states);
    }
  });

  it.skipIf(!hasLayoutBrowser)(
    'measures and hit-tests every day target at both widths and themes',
    async () => {
      const { container } = renderHours(locale);
      for (const width of [375, 1440]) {
        await withLayoutPage(container.innerHTML, { width, height: 900 }, async (page) => {
          for (const theme of ['light', 'dark']) {
            await page.evaluate((value) => (document.documentElement.className = value), theme);
            const switches = page.getByRole('switch');
            for (let index = 0; index < 7; index++) {
              const control = switches.nth(index);
              const label = control.locator('..');
              const target = (await label.boundingBox())!;
              const track = (await control.boundingBox())!;
              expect(target.height).toBeGreaterThanOrEqual(44);
              expect(target.width).toBeGreaterThanOrEqual(44);
              expect(track.width).toBe(36);
              expect(track.height).toBe(20);
              await control.evaluate((element) => {
                element.removeAttribute('data-activated');
                element.addEventListener('click', () =>
                  element.setAttribute('data-activated', 'true')
                );
              });
              await page.mouse.click(track.x + track.width / 2, target.y + 1);
              expect(await control.getAttribute('data-activated')).toBe('true');
            }
          }
        });
      }
    },
    30_000
  );
});
