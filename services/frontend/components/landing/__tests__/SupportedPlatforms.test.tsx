import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { SupportedPlatforms } from '../SupportedPlatforms';
import ru from '@/messages/ru.json';
import en from '@/messages/en.json';

const state = vi.hoisted(() => ({
  data: undefined as { id: string; status: string }[] | undefined,
  isPending: false,
  isError: false,
}));
vi.mock('@/lib/hooks/usePlatforms', () => ({ usePlatforms: () => state }));
afterEach(cleanup);

describe('landing platform availability', () => {
  it.each(['ru', 'en'] as const)(
    'uses full names and only confirmed availability in %s',
    (locale) => {
      (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
        locale
      );
      const copy = (locale === 'ru' ? ru : en).landing;
      Object.assign(state, { data: undefined, isPending: true, isError: false });
      const { rerender } = render(<SupportedPlatforms />);
      expect(screen.queryByText(copy.platformStatus.have)).toBeNull();
      expect(screen.getAllByText(copy.platformStatus.loading)).toHaveLength(7);
      Object.assign(state, { isPending: false, isError: true });
      rerender(<SupportedPlatforms />);
      expect(screen.getAllByText(copy.platformStatus.unknown)).toHaveLength(7);
      Object.assign(state, {
        isError: false,
        data: [
          { id: 'telegram', status: 'active' },
          { id: 'vk', status: 'oauth_not_configured' },
          { id: 'google_business', status: 'coming_soon' },
        ],
      });
      rerender(<SupportedPlatforms />);
      expect(screen.getAllByText(copy.platformStatus.have)).toHaveLength(1);
      for (const [id, key] of [
        ['telegram', 'have'],
        ['vk', 'unconfigured'],
        ['google_business', 'soon'],
        ['yandex_business', 'unknown'],
        ['instagram', 'unlisted'],
      ] as const) {
        const row = screen
          .getByRole('heading', { name: copy.platforms[id].display })
          .closest('li')!;
        expect(within(row).getByText(copy.platformStatus[key])).toBeVisible();
      }
    }
  );
});
