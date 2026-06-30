import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { AxiosRequestConfig } from 'axios';
import type { ReactNode } from 'react';

import { ChatHeader } from '../ChatHeader';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

// Every observer of conversationsQueryKey must fetch the full window
// (limit:100). In TanStack Query v5 one shared key backs a single Query with
// ONE queryFn — the last observer to call setOptions wins — so a frequent
// invalidate may refetch through ChatHeader's queryFn. If that queryFn omits
// the limit the server caps the list at its default (20) and the sidebar
// silently loses chats. This records every GET /conversations request config
// and asserts limit:100 is always present.

const BUSINESS_ID = 'biz-test';

const getSpy = vi.fn<(path: string, config?: AxiosRequestConfig) => Promise<{ data: unknown }>>();

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string }) => unknown) =>
    selector({ activeBusinessId: BUSINESS_ID }),
}));

vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: getSpy,
  }),
}));

vi.mock('@/hooks/useConversations', () => ({
  usePinConversation: () => ({ mutate: vi.fn(), isPending: false }),
  useUnpinConversation: () => ({ mutate: vi.fn(), isPending: false }),
  conversationsQueryKey: (bizId: string | null) => ['businesses', bizId, 'conversations'] as const,
}));

function renderHeader() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
  return render(<ChatHeader conversationId="c1" />, { wrapper });
}

describe('ChatHeader — full-window conversation fetch', () => {
  beforeEach(() => {
    getSpy.mockReset();
    getSpy.mockResolvedValue({ data: [] });
  });

  it('requests limit:100 on every GET /conversations it issues', async () => {
    renderHeader();

    await waitFor(() => {
      expect(getSpy).toHaveBeenCalled();
    });

    const rootCalls = getSpy.mock.calls.filter(
      ([path]) => path === BIZ_API_PATHS.CONVERSATIONS.ROOT
    );
    expect(rootCalls.length).toBeGreaterThan(0);
    for (const [, config] of rootCalls) {
      expect(config?.params?.limit).toBe(100);
    }
  });
});
