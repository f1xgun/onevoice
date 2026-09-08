import { beforeEach, expect, it, vi } from 'vitest';
import { AxiosError } from 'axios';
import { api } from '@/lib/api';
import { refreshAccessToken } from '@/lib/api/authFetch';
import { trackEvent } from '@/lib/telemetry';
import { useBusinessStore } from '@/lib/stores/business';

vi.mock('@/lib/api/authFetch', () => ({ refreshAccessToken: vi.fn() }));
vi.mock('@/lib/telemetry', () => ({ trackEvent: vi.fn() }));

beforeEach(() => {
  vi.clearAllMocks();
  useBusinessStore.getState().clear();
});

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

it('attributes a late API error to the business captured when the request started', async () => {
  const businessA = '11111111-1111-1111-1111-111111111111';
  const businessB = '22222222-2222-2222-2222-222222222222';
  useBusinessStore.getState().setActive(businessA);
  const adapter = vi.fn(async (config) => {
    useBusinessStore.getState().setActive(businessB);
    throw new AxiosError('Failed', 'ERR_BAD_REQUEST', config, undefined, {
      status: 500,
      statusText: 'Failed',
      data: {},
      headers: { 'x-correlation-id': 'request-a' },
      config,
    });
  });

  await expect(api.get(`/businesses/${businessA}/integrations`, { adapter })).rejects.toMatchObject(
    { response: { status: 500 } }
  );
  await vi.waitFor(() =>
    expect(trackEvent).toHaveBeenCalledWith(
      'api_error',
      `500 GET /businesses/${businessA}/integrations`,
      {
        businessId: businessA,
        correlationId: 'request-a',
        metadata: {
          status: '500',
          url: `/businesses/${businessA}/integrations`,
        },
      }
    )
  );
});

it('keeps a global API request global while a business is active', async () => {
  useBusinessStore.getState().setActive('11111111-1111-1111-1111-111111111111');
  const adapter = vi.fn(async (config) => {
    throw new AxiosError('Failed', 'ERR_BAD_REQUEST', config, undefined, {
      status: 500,
      statusText: 'Failed',
      data: {},
      headers: { 'x-correlation-id': 'global-request' },
      config,
    });
  });

  await expect(api.get('/businesses', { adapter })).rejects.toMatchObject({
    response: { status: 500 },
  });
  await vi.waitFor(() =>
    expect(trackEvent).toHaveBeenCalledWith('api_error', '500 GET /businesses', {
      businessId: null,
      correlationId: 'global-request',
      metadata: { status: '500', url: '/businesses' },
    })
  );
});
