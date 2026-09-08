import { webcrypto } from 'node:crypto';
import { createElement, type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { flushTelemetry } from '@/lib/telemetry';
import { mockSSEResponse, sseLine } from '@/test-utils/sse-mock';
import type { PendingApproval } from '@/types/chat';

import { useConversationFlow } from '../useConversationFlow';

const batch: PendingApproval = {
  batchId: 'batch-1',
  status: 'pending',
  createdAt: '2026-09-08T00:00:00Z',
  calls: [
    {
      callId: 'tc_a',
      toolName: 'telegram__send_channel_post',
      args: { text: 'PRIVATE POST' },
      editableFields: ['text'],
      floor: 'manual',
    },
    {
      callId: 'tc_b',
      toolName: 'yandex_business__reply_review',
      args: { text: 'PRIVATE REVIEW' },
      editableFields: ['text'],
      floor: 'manual',
    },
  ],
};

describe('saved approval edits', () => {
  beforeEach(() => {
    useAuthStore.setState({ accessToken: 'test-token' });
    useBusinessStore.setState({ activeBusinessId: 'biz-test' });
    vi.stubGlobal('crypto', webcrypto);
  });
  afterEach(async () => {
    await flushTelemetry();
    vi.unstubAllGlobals();
    useAuthStore.setState({ accessToken: null });
  });

  it('records edits only after resolve succeeds, before resume finishes, for posts and reviews', async () => {
    const events: unknown[] = [];
    let acceptResolve!: (response: Response) => void;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.endsWith('/messages'))
          return new Response(JSON.stringify({ messages: [], pendingApprovals: [batch] }));
        if (url.endsWith('/resolve'))
          return new Promise<Response>((resolve) => {
            acceptResolve = resolve;
          });
        if (url.endsWith('/resume'))
          return mockSSEResponse([sseLine({ type: 'error', content: 'PRIVATE ERROR' })]);
        if (url.endsWith('/telemetry')) {
          events.push(...JSON.parse(init!.body as string));
          return new Response(null, { status: 204 });
        }
        throw new Error('Unexpected request');
      })
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    function Wrapper({ children }: { children: ReactNode }) {
      return createElement(QueryClientProvider, { client: qc }, children);
    }
    const { result } = renderHook(() => useConversationFlow({ conversationId: 'conv-1' }), {
      wrapper: Wrapper,
    });
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());
    let resolved!: Promise<void>;
    act(() => {
      resolved = result.current.resolveApproval(
        batch.calls.map((call) => ({
          id: call.callId,
          action: 'edit',
          edited_args: { text: 'PRIVATE EDIT private@example.com' },
        }))
      );
    });
    await flushTelemetry();
    expect(events).toEqual([]);
    await act(async () => {
      acceptResolve(new Response('{}'));
      await resolved;
    });
    await waitFor(async () => {
      await flushTelemetry();
      expect(events).toHaveLength(2);
    });
    expect(events).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          action: 'edit_saved',
          correlationId: 'e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d',
          metadata: expect.objectContaining({ kind: 'post' }),
        }),
        expect.objectContaining({
          action: 'edit_saved',
          metadata: expect.objectContaining({ kind: 'review_reply' }),
        }),
      ])
    );
    expect(JSON.stringify(events)).not.toMatch(/PRIVATE|private@|text|args/);
  });

  it('does not record failed or unchanged edits as saved', async () => {
    const events: unknown[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.endsWith('/messages')) {
          return new Response(JSON.stringify({ messages: [], pendingApprovals: [batch] }));
        }
        if (url.endsWith('/resolve')) return new Response('{}', { status: 400 });
        if (url.endsWith('/telemetry')) {
          events.push(...JSON.parse(init!.body as string));
          return new Response(null, { status: 204 });
        }
        throw new Error('Unexpected request');
      })
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    function Wrapper({ children }: { children: ReactNode }) {
      return createElement(QueryClientProvider, { client: qc }, children);
    }
    const { result } = renderHook(() => useConversationFlow({ conversationId: 'conv-1' }), {
      wrapper: Wrapper,
    });
    await waitFor(() => expect(result.current.pendingApproval).not.toBeNull());

    await act(async () => {
      await result.current.resolveApproval([
        { id: 'tc_a', action: 'edit', edited_args: { text: 'PRIVATE FAILED EDIT' } },
        { id: 'tc_b', action: 'edit', edited_args: { text: 'PRIVATE REVIEW' } },
      ]);
    });
    await flushTelemetry();

    expect(events).toEqual([]);
  });
});
