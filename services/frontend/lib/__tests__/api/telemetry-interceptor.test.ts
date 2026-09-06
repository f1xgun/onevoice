import { expect, it, vi } from 'vitest';
import { AxiosError } from 'axios';
import { api } from '@/lib/api';
import { refreshAccessToken } from '@/lib/api/authFetch';
import { trackEvent } from '@/lib/telemetry';

vi.mock('@/lib/api/authFetch', () => ({ refreshAccessToken: vi.fn() }));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));

it('rejects telemetry 401 without refreshing, replaying, logging recursively or navigating', async () => {
  const before = window.location.href;
  const adapter = vi.fn(async (config) => {
    throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
      status: 401,
      statusText: 'Unauthorized',
      data: {},
      headers: { 'x-correlation-id': 'test-correlation' },
      config,
    });
  });
  await expect(api.post('/telemetry', [], { adapter })).rejects.toMatchObject({
    response: { status: 401 },
  });
  expect(adapter).toHaveBeenCalledTimes(1);
  expect(refreshAccessToken).not.toHaveBeenCalled();
  expect(trackEvent).not.toHaveBeenCalled();
  expect(window.location.href).toBe(before);
});
