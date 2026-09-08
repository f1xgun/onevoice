import { expect, it } from 'vitest';

import en from '@/messages/en.json';
import ru from '@/messages/ru.json';
import responses from '@/test/fixtures/channel-responses.json';

import { connectionStatus } from '../connectionStatus';

it.each([
  [{ businessId: null }, 'chooseOrganization'],
  [{ pending: true }, 'connectionLoading'],
  [{ error: true, integrations: responses.active.body }, 'connectionUnknown'],
  [{ integrations: undefined }, 'connectionUnknown'],
  [{ integrations: responses.empty.body }, 'notConnected'],
  [{ integrations: responses.active.body }, 'connected'],
  [{ integrations: responses.inconclusive.body }, 'connected'],
  [{ integrations: responses.healthy.body }, 'connected'],
  [{ integrations: responses.degraded.body }, 'connected'],
  [{ integrations: responses.tokenExpired.body }, 'connectionAttention'],
  [{ integrations: responses.broken.body }, 'connectionAttention'],
  [{ integrations: [{ status: 'future-state' }] }, 'connectionUnknown'],
  [{ integrations: [{ status: 'inactive' }] }, 'connectionUnknown'],
  [{ integrations: [{ status: 'error' }] }, 'connectionUnknown'],
  [{ integrations: [{ status: '' }] }, 'connectionUnknown'],
  [{ integrations: [{}] }, 'connectionUnknown'],
  [
    { integrations: [...responses.active.body, ...responses.tokenExpired.body] },
    'connectionAttention',
  ],
  [
    { integrations: [...responses.tokenExpired.body, ...responses.active.body] },
    'connectionAttention',
  ],
  [{ integrations: [...responses.active.body, { status: 'future' }] }, 'connectionUnknown'],
] as const)('describes the API evidence %j', (state, expected) => {
  const key = connectionStatus({ businessId: 'org', pending: false, error: false, ...state });
  expect(key).toBe(expected);
  expect(ru.nav[key]).toBeTruthy();
  expect(en.nav[key]).toBeTruthy();
});
