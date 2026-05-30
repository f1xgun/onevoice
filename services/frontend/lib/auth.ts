import { create } from 'zustand';

export interface User {
  id: string;
  email: string;
  name: string;
  // /auth/me returns emailVerified + (when false)
  // an ISO8601 deadline (created_at + 7 days). The persistent
  // VerificationBanner renders when emailVerified === false.
  emailVerified?: boolean;
  emailVerificationDeadline?: string;
  // trip-wire reused by stacking rule so
  // the ReConsentModal yields to EmailVerifiedRequiredModal when both
  // would fire on the same session. Backend doesn't expose this as a
  // separate field — the modal precedence layer reads
  // `!emailVerified && emailVerificationDeadline` as "requires
  // verification" per existing convention.
  requiresEmailVerification?: boolean;
  // non-null when the user is inside the 30-day
  // deletion grace window. DeletionGraceBanner mounts when this is set.
  accountDeletion?: AccountDeletionInfo | null;
  // non-null when the user's user_consents
  // rows are stale relative to pkg/legalconfig.CurrentVersion. The
  // ReConsentModal renders one diff card per entry and locks the
  // session until the user POSTs to /auth/consents (accept) or
  // /auth/logout (logout).
  requiresReconsent?: ReConsentRequirement | null;
}

export interface AccountDeletionInfo {
  requestedAt: string;
  scheduledDeletionAt: string;
  canRestoreUntil: string;
}

export interface PolicyDiff {
  slug: 'tos' | 'privacy' | 'pdn';
  oldVersion: string;
  newVersion: string;
  sha256: string;
}

export interface ReConsentRequirement {
  policies: PolicyDiff[];
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
