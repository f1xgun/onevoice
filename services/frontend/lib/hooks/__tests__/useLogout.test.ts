import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useLogout } from '../useLogout';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { queryClient } from '@/lib/queryClient';
import { api } from '@/lib/api';

vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }));
const owner = { id: 'a', name: 'Owner', email: 'a@example.test' };

beforeEach(() => {
  useAuthStore.getState().logout();
  queryClient.clear();
  vi.clearAllMocks();
});

describe('tenant identity lifecycle', () => {
  it.each([false, true])(
    'clears persisted tenant and cache on logout (offline=%s)',
    async (offline) => {
      useAuthStore.getState().setAuth(owner, 'test');
      useBusinessStore.getState().setActive('org-a');
      queryClient.setQueryData(['businesses'], [{ id: 'org-a' }]);
      vi.mocked(api.post).mockImplementation(async () => {
        expect(useAuthStore.getState().isAuthenticated).toBe(true);
        if (offline) throw new Error('offline');
        return { data: {} };
      });
      await useLogout()();
      expect(useBusinessStore.getState().activeBusinessId).toBeNull();
      expect(
        JSON.parse(localStorage.getItem('onevoice.business')!).state.activeBusinessId
      ).toBeNull();
      expect(queryClient.getQueryData(['businesses'])).toBeUndefined();
      useAuthStore.getState().setAuth({ ...owner, id: 'b' }, 'test-b');
      expect(useBusinessStore.getState().activeBusinessId).toBeNull();
    }
  );

  it('restores only selections belonging to the authenticated user', () => {
    useAuthStore.getState().setAuth(owner, 'test');
    useBusinessStore.getState().setActive('org-a');
    useAuthStore.getState().setAuth(owner, 'refreshed');
    expect(useBusinessStore.getState().activeBusinessId).toBe('org-a');
    useAuthStore.getState().setAuth({ ...owner, id: 'b' }, 'test-b');
    expect(useBusinessStore.getState().activeBusinessId).toBeNull();
  });

  it('discards legacy persisted selections without a user identity', () => {
    useBusinessStore.setState({ userId: null, activeBusinessId: 'legacy' });
    useAuthStore.getState().setAuth(owner, 'test');
    expect(useBusinessStore.getState().activeBusinessId).toBeNull();
  });
});
