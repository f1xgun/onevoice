import { expect, it } from 'vitest';
import en from '@/messages/en.json';
import ru from '@/messages/ru.json';
import { connectionStatus } from '../connectionStatus';

it.each([
  [{ businessId: null }, 'chooseOrganization'],
  [{ pending: true }, 'connectionLoading'],
  [{ error: true, status: 'active' }, 'connectionUnknown'],
  [{ status: 'active' }, 'connected'],
  [{ status: undefined }, 'notConnected'],
  [{ status: 'inactive' }, 'notConnected'],
  [{ status: 'error' }, 'connectionAttention'],
  [{ status: 'future-state' }, 'connectionAttention'],
] as const)('describes actual connection state %j', (state, expected) => {
  const key = connectionStatus({ businessId: 'org', pending: false, error: false, ...state });
  expect(key).toBe(expected);
  expect(ru.nav[key]).toBeTruthy();
  expect(en.nav[key]).toBeTruthy();
});
