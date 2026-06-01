// __tests__/PlatformCard.test.tsx
//
// regression guard for the Preview badge slot on
// <PlatformCard>. The badge is the user-visible signal that an
// integration (today only `google_business`) is partially-implemented —
// it shows up only when `isPreview={true}` is explicitly passed, uses
// the same translation key for both `title` (browser tooltip) and
// `aria-label` (screen-reader announcement), and must NOT render for
// the default (Telegram/VK/Yandex.Business) path.
//
// Notes on test plumbing:
// - The global next-intl stub in `vitest.setup.ts` resolves
// `useTranslations('integrations.platformCard')` against the real
// `messages/{ru,en}.json` bundles, so we assert against the actual
// localized copy rather than mocking string returns.
// - Default test locale is `ru`; we flip to `en` via
// `globalThis.__setTestLocale('en')` for the EN assertions.
// - `PlatformCard` calls `useQueryClient` (TanStack Query) on every
// render, so the wrapper provides a fresh QueryClient per render.
// - With `integrations={[]}` the inner `ChannelList` is not mounted,
// which means `usePermission` is not exercised — no extra mocks
// needed beyond the business store.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { PlatformCard } from '@/components/integrations/PlatformCard';

declare global {
  // eslint-disable-next-line no-var
  var __setTestLocale: (locale: 'ru' | 'en') => void;
}

// `useBusinessStore` is called at the top of `PlatformCard` to read
// `activeBusinessId`. Returning a stable stub avoids spurious refetches
// during the test (no network calls are exercised here anyway).
vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector?: (s: { activeBusinessId: string }) => unknown) => {
    const state = { activeBusinessId: 'test-biz-id' };
    return selector ? selector(state) : state;
  },
}));

// `bizApi` is only invoked from the inner `refreshTelegramLinkedGroup`
// closure, which is unreachable when `integrations={[]}`. Mock kept as
// a safety net so a future code change can't accidentally hit the
// network from inside this test.
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  }),
}));

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

// Minimal valid prop set — every required field on the `Props`
// interface plus the two permission booleans, both set to `true` to
// match the production code path that exercises the new badge.
const baseProps = {
  platform: 'google_business',
  label: 'Google Business',
  description: 'Google Business Profile (preview)',
  integrations: [],
  onConnect: vi.fn(),
  onDisconnect: vi.fn(),
  canConnect: true,
  canDisconnect: true,
} as const;

describe('<PlatformCard /> Preview badge', () => {
  afterEach(() => {
    cleanup();
    // The global setup file resets the locale to `ru` in its own
    // `afterEach`; explicit local reset documents intent and survives
    // any future reordering of teardown hooks.
    globalThis.__setTestLocale('ru');
  });

  it('renders the RU Preview badge with localized title + aria-label when isPreview={true}', () => {
    render(
      <Wrapper>
        <PlatformCard {...baseProps} isPreview />
      </Wrapper>
    );

    const badge = screen.getByText('Предпросмотр');
    expect(badge).toBeInTheDocument();

    const expectedTooltip =
      'Сейчас работают только отзывы (получение и ответ). Информация о компании, посты, медиа и метрики появятся в следующих обновлениях.';
    expect(badge).toHaveAttribute('title', expectedTooltip);
    expect(badge).toHaveAttribute('aria-label', expectedTooltip);
  });

  it('renders the EN Preview badge with localized title + aria-label when locale is en', () => {
    globalThis.__setTestLocale('en');

    render(
      <Wrapper>
        <PlatformCard {...baseProps} isPreview />
      </Wrapper>
    );

    const badge = screen.getByText('Preview');
    expect(badge).toBeInTheDocument();

    const expectedTooltip =
      'Only review operations (read and reply) work today. Profile info, posts, media, and metrics are coming in upcoming releases.';
    expect(badge).toHaveAttribute('title', expectedTooltip);
    expect(badge).toHaveAttribute('aria-label', expectedTooltip);
  });

  it('does NOT render a Preview badge when isPreview is omitted (default)', () => {
    render(
      <Wrapper>
        <PlatformCard {...baseProps} />
      </Wrapper>
    );

    // Neither locale's label may surface — `isPreview` defaults to false
    // and the badge slot must be skipped entirely for the regression
    // guard to mean anything to first-class integrations.
    expect(screen.queryByText('Предпросмотр')).toBeNull();
    expect(screen.queryByText('Preview')).toBeNull();
  });

  it('does NOT render a Preview badge when isPreview={false} is explicit', () => {
    render(
      <Wrapper>
        <PlatformCard {...baseProps} isPreview={false} />
      </Wrapper>
    );

    expect(screen.queryByText('Предпросмотр')).toBeNull();
    expect(screen.queryByText('Preview')).toBeNull();
  });
});
