import { webcrypto } from 'node:crypto';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { api } from '@/lib/api';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { flushTelemetry } from '@/lib/telemetry';

import ReviewsPage from '../page';

vi.mock('@/lib/hooks/usePermission', () => ({ usePermission: () => ({ allowed: true }) }));

const originalAdapter = api.defaults.adapter;
const review = {
  id: 'rev-retry',
  businessId: 'organization',
  platform: 'telegram',
  authorName: 'private@example.com',
  text: 'PRIVATE REVIEW',
  draftReply: 'PRIVATE DRAFT',
  replyStatus: 'pending',
  draftStatus: 'ready',
  createdAt: '2026-09-06T10:00:00Z',
};

describe('review approval telemetry', () => {
  beforeEach(() => {
    useAuthStore.setState({ accessToken: 'test-token' });
    useBusinessStore.setState({ activeBusinessId: 'organization' });
    vi.stubGlobal('crypto', webcrypto);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
  });
  afterEach(async () => {
    await flushTelemetry();
    api.defaults.adapter = originalAdapter;
    useAuthStore.setState({ accessToken: null });
    vi.unstubAllGlobals();
  });

  it('records review impressions without duplicating server-owned edit_saved', async () => {
    let save!: () => void;
    api.defaults.adapter = async (config) => {
      if (config.method === 'put')
        await new Promise<void>((resolve) => {
          save = resolve;
        });
      return {
        data: config.method === 'get' ? [review] : {},
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      };
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <ReviewsPage />
      </QueryClientProvider>
    );
    await screen.findByText('PRIVATE DRAFT');
    await waitFor(async () => {
      await flushTelemetry();
      expect(fetch).toHaveBeenCalled();
    });
    function events() {
      return vi.mocked(fetch).mock.calls.flatMap(([, init]) => JSON.parse(init!.body as string));
    }
    expect(
      events()
        .map((event) => event.action)
        .sort()
    ).toEqual(['approval_shown', 'draft_shown']);
    fireEvent.click(screen.getByRole('button', { name: 'Отредактировать' }));
    fireEvent.change(screen.getByRole('textbox'), {
      target: { value: 'PRIVATE EDIT private@example.com' },
    });
    fireEvent.click(screen.getAllByRole('button', { name: 'Отправить' }).at(-1)!);
    await waitFor(() => expect(save).toBeDefined());
    await flushTelemetry();
    expect(events().filter((event) => event.action === 'edit_saved')).toHaveLength(0);
    save();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await flushTelemetry();
    expect(events().filter((event) => event.action === 'edit_saved')).toHaveLength(0);
    for (const event of events()) {
      expect(event.correlationId).toBe(
        '23fb79b48147b1a1cefe8c99c56039c3c8365ef7588a15ac7db23a54e3f5cd62'
      );
      expect(event.metadata.kind).toBe('review_reply');
    }
    expect(JSON.stringify(events())).not.toMatch(/PRIVATE|private@|author|args|replyText/);
  });
});
