import { describe, expect, it, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import React, { type ReactNode } from 'react';
import { useDeleteConversation } from '../useConversations';
import { useDeleteProject } from '../useProjects';

// Both deleteConversation and deleteProject funnel through bizApi(...).delete.
const bizApiDelete = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: () => ({
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: (...args: unknown[]) => bizApiDelete(...args),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'test-biz-id' }),
}));

const BIZ = 'test-biz-id';
const conversationsKey = ['businesses', BIZ, 'conversations'];
const conversationDetailKey = ['businesses', BIZ, 'conversations', 'c-1'];
const projectsKey = ['businesses', BIZ, 'projects'];
const projectDetailKey = ['businesses', BIZ, 'projects', 'p-1'];

function setup() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const wrapper = ({ children }: { children: ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children);
  return { qc, wrapper };
}

// Regression guard for N6. A delete must not cause the just-deleted resource's
// own detail query — still mounted as an active observer on /chat/[id] or
// /projects/[id] until the route navigates away — to refetch, which hit the
// deleted id and surfaced a transient 404. Reverting to a non-exact invalidate
// (or re-adding removeQueries on the project side) makes the active detail
// observer fetch a second time and fails these tests.
describe('delete mutations do not refetch the just-deleted resource (N6)', () => {
  beforeEach(() => bizApiDelete.mockReset());

  it('useDeleteConversation leaves the open chat detail query untouched', async () => {
    bizApiDelete.mockResolvedValue({ data: undefined });
    const { wrapper } = setup();
    const detailFetch = vi.fn().mockResolvedValue({ id: 'c-1' });

    const { result } = renderHook(
      () => {
        useQuery({ queryKey: conversationDetailKey, queryFn: detailFetch });
        return useDeleteConversation();
      },
      { wrapper }
    );

    await waitFor(() => expect(detailFetch).toHaveBeenCalledTimes(1));

    await act(async () => {
      await result.current.mutateAsync('c-1');
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(detailFetch).toHaveBeenCalledTimes(1);
  });

  it('useDeleteConversation invalidates the list exactly (+ projects)', async () => {
    bizApiDelete.mockResolvedValue({ data: undefined });
    const { qc, wrapper } = setup();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useDeleteConversation(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync('c-1');
    });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: conversationsKey, exact: true });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectsKey });
  });

  it('useDeleteProject leaves the project detail query untouched', async () => {
    bizApiDelete.mockResolvedValue({ data: { deletedConversations: 0, deletedMessages: 0 } });
    const { wrapper } = setup();
    const detailFetch = vi.fn().mockResolvedValue({ id: 'p-1', businessId: BIZ, name: 'P' });

    const { result } = renderHook(
      () => {
        useQuery({ queryKey: projectDetailKey, queryFn: detailFetch });
        return useDeleteProject();
      },
      { wrapper }
    );

    await waitFor(() => expect(detailFetch).toHaveBeenCalledTimes(1));

    await act(async () => {
      await result.current.mutateAsync('p-1');
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(detailFetch).toHaveBeenCalledTimes(1);
  });

  it('useDeleteProject invalidates the list exactly and never removeQueries', async () => {
    bizApiDelete.mockResolvedValue({ data: { deletedConversations: 0, deletedMessages: 0 } });
    const { qc, wrapper } = setup();
    const invalidate = vi.spyOn(qc, 'invalidateQueries');
    const remove = vi.spyOn(qc, 'removeQueries');
    const { result } = renderHook(() => useDeleteProject(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync('p-1');
    });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectsKey, exact: true });
    expect(remove).not.toHaveBeenCalled();
  });
});
