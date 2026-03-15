import { describe, it, expect, beforeEach } from 'vitest';
import { useAuthStore } from '../auth';
import type { User } from '../auth';

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, accessToken: null, isAuthenticated: false });
  });

  it('sets user and token on login', () => {
    const user: User = { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' };
    useAuthStore.getState().setAuth(user, 'access-token');

    expect(useAuthStore.getState().user).toEqual(user);
    expect(useAuthStore.getState().accessToken).toBe('access-token');
    expect(useAuthStore.getState().isAuthenticated).toBe(true);
  });

  it('clears state on logout', () => {
    const logoutUser: User = { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' };
    useAuthStore.getState().setAuth(logoutUser, 'access-token');
    useAuthStore.getState().logout();

    expect(useAuthStore.getState().user).toBeNull();
    expect(useAuthStore.getState().accessToken).toBeNull();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });
});
