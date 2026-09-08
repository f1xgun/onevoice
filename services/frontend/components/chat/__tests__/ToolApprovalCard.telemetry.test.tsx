import { webcrypto } from 'node:crypto';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';

import { useAuthStore } from '@/lib/auth';
import { flushTelemetry } from '@/lib/telemetry';
import type { PendingApproval } from '@/types/chat';

import { ToolApprovalCard } from '../ToolApprovalCard';

const batch: PendingApproval = {
  batchId: 'batch-1',
  status: 'pending',
  createdAt: '2026-09-08T00:00:00Z',
  calls: [
    {
      callId: 'tc_a',
      toolName: 'telegram__send_channel_post',
      args: { text: 'PRIVATE POST', channel_id: 'private@example.com' },
      editableFields: ['text'],
      floor: 'manual',
    },
    {
      callId: 'tc_b',
      toolName: 'yandex_business__reply_review',
      args: { text: 'PRIVATE REPLY', review_id: 'private@example.com' },
      editableFields: ['text'],
      floor: 'manual',
    },
  ],
};

describe('approval impressions', () => {
  beforeEach(() => {
    useAuthStore.setState({ accessToken: 'test-token' });
    vi.stubGlobal('crypto', webcrypto);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
  });
  afterEach(async () => {
    await flushTelemetry();
    vi.unstubAllGlobals();
    useAuthStore.setState({ accessToken: null });
  });

  it('correlates post and review impressions once per mounted draft, without content or PII', async () => {
    const submit = vi.fn();
    const view = render(
      <StrictMode>
        <ToolApprovalCard batch={batch} onSubmit={submit} />
      </StrictMode>
    );
    await waitFor(async () => {
      await flushTelemetry();
      expect(fetch).toHaveBeenCalled();
    });
    const events = vi
      .mocked(fetch)
      .mock.calls.flatMap(([, init]) => JSON.parse(init!.body as string));
    expect(events).toHaveLength(4);
    expect(
      events
        .filter((event) => event.metadata.kind === 'review_reply')
        .map((event) => event.action)
        .sort()
    ).toEqual(['approval_shown', 'draft_shown']);
    expect(events.filter((event) => event.metadata.kind === 'post')).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          action: 'draft_shown',
          correlationId: 'e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d',
        }),
        expect.objectContaining({
          action: 'approval_shown',
          correlationId: 'e505fde9103c0500bf2fe449ca9c6023c15e3edc0341461287fdca4e3215b16d',
        }),
      ])
    );
    expect(JSON.stringify(events)).not.toMatch(/PRIVATE|private@|channel_id|review_id|toolName/);
    view.rerender(
      <StrictMode>
        <ToolApprovalCard batch={{ ...batch }} onSubmit={submit} />
      </StrictMode>
    );
    await flushTelemetry();
    expect(submit).not.toHaveBeenCalled();
    expect(
      vi.mocked(fetch).mock.calls.flatMap(([, init]) => JSON.parse(init!.body as string))
    ).toHaveLength(4);
  });
});
