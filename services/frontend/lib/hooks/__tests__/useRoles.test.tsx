import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

vi.mock('@/lib/api/roles', () => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
}));

import { useRoles, useCreateRole, useUpdateRole, useDeleteRole } from '@/lib/hooks/useRoles';
import { listRoles, createRole, updateRole, deleteRole } from '@/lib/api/roles';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import type { Role } from '@/lib/schemas';

const mockedListRoles = vi.mocked(listRoles);
const mockedCreateRole = vi.mocked(createRole);
const mockedUpdateRole = vi.mocked(updateRole);
const mockedDeleteRole = vi.mocked(deleteRole);

const fakeRole: Role = {
  id: 'role-1',
  business_id: 'biz-1',
  name: 'Marketing',
  description: 'desc',
  permissions: ['business.read'],
  is_system: false,
};

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

beforeEach(() => {
  mockedListRoles.mockReset();
  mockedCreateRole.mockReset();
  mockedUpdateRole.mockReset();
  mockedDeleteRole.mockReset();
});

describe('useRoles', () => {
  it('fetches roles for businessId with ROLES(bizId) cache key', async () => {
    mockedListRoles.mockResolvedValue([fakeRole]);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useRoles('biz-1'), { wrapper: makeWrapper(qc) });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([fakeRole]);
    expect(mockedListRoles).toHaveBeenCalledWith('biz-1');
    // Confirm the entry landed at the expected key
    expect(qc.getQueryData(QUERY_KEYS.ROLES('biz-1'))).toEqual([fakeRole]);
  });

  it('does not fetch when businessId is null', () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderHook(() => useRoles(null), { wrapper: makeWrapper(qc) });
    expect(mockedListRoles).not.toHaveBeenCalled();
  });
});

describe('useCreateRole', () => {
  it('invalidates ROLES + PERMISSIONS on success', async () => {
    mockedCreateRole.mockResolvedValue(fakeRole);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useCreateRole('biz-1'), {
      wrapper: makeWrapper(qc),
    });
    await result.current.mutateAsync({
      name: 'X',
      description: 'd',
      permissions: ['business.read'],
    });
    const keysInvalidated = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.ROLES('biz-1'));
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.PERMISSIONS('biz-1'));
  });
});

describe('useUpdateRole', () => {
  it('invalidates ROLES + PERMISSIONS on success', async () => {
    mockedUpdateRole.mockResolvedValue(fakeRole);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useUpdateRole('biz-1'), {
      wrapper: makeWrapper(qc),
    });
    await result.current.mutateAsync({
      roleId: 'role-1',
      name: 'X',
      description: 'd',
      permissions: ['business.read'],
    });
    const keysInvalidated = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.ROLES('biz-1'));
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.PERMISSIONS('biz-1'));
    expect(mockedUpdateRole).toHaveBeenCalledWith('biz-1', 'role-1', {
      name: 'X',
      description: 'd',
      permissions: ['business.read'],
    });
  });
});

describe('useDeleteRole', () => {
  it('invalidates ROLES + PERMISSIONS + MEMBERS on success', async () => {
    mockedDeleteRole.mockResolvedValue(undefined);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    const { result } = renderHook(() => useDeleteRole('biz-1'), {
      wrapper: makeWrapper(qc),
    });
    await result.current.mutateAsync({ roleId: 'role-1', reassignTo: 'role-2' });
    const keysInvalidated = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.ROLES('biz-1'));
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.PERMISSIONS('biz-1'));
    expect(keysInvalidated).toContainEqual(QUERY_KEYS.MEMBERS('biz-1'));
    expect(mockedDeleteRole).toHaveBeenCalledWith('biz-1', 'role-1', 'role-2');
  });

  it('forwards null reassignTo to the API wrapper', async () => {
    mockedDeleteRole.mockResolvedValue(undefined);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useDeleteRole('biz-1'), {
      wrapper: makeWrapper(qc),
    });
    await result.current.mutateAsync({ roleId: 'role-1', reassignTo: null });
    expect(mockedDeleteRole).toHaveBeenCalledWith('biz-1', 'role-1', null);
  });
});
