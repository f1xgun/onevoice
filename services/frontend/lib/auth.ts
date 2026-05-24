import { create } from 'zustand';

export interface User {
  id: string;
  email: string;
  name: string;
  // Phase 21-03 (ACCT-02): /auth/me returns emailVerified + (when false)
  // an ISO8601 deadline (created_at + 7 days). The persistent
  // VerificationBanner renders when emailVerified === false.
  emailVerified?: boolean;
  emailVerificationDeadline?: string;
}

interface AuthState {
  user: User | null;
  accessToken: string | null;
  isAuthenticated: boolean;
  setAuth: (user: User, accessToken: string) => void;
  setAccessToken: (token: string) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  accessToken: null,
  isAuthenticated: false,

  setAuth: (user, accessToken) => {
    set({ user, accessToken, isAuthenticated: true });
  },

  setAccessToken: (token) => {
    set({ accessToken: token, isAuthenticated: !!token });
  },

  logout: () => {
    set({ user: null, accessToken: null, isAuthenticated: false });
  },
}));
