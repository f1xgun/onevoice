'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/**
 * Records an organization that was just soft-deleted so a grace banner can
 * offer a Restore action. Soft-deleted organizations are invisible to reads
 * (GetByID filters deleted_at IS NULL), so the deletion state cannot be
 * re-derived from the server — this client-side signal bridges the gap during
 * the grace window. Cleared on restore or when dismissed.
 */
export interface PendingBusinessDeletion {
  id: string;
  name: string;
  scheduledDeletionAt: string;
}

export interface BusinessDeletionState {
  pending: PendingBusinessDeletion | null;
  setPending: (p: PendingBusinessDeletion) => void;
  clear: () => void;
}

const DELETION_GRACE_DAYS = 30;
const HOURS_PER_DAY = 24;
const MINUTES_PER_HOUR = 60;
const SECONDS_PER_MINUTE = 60;
const MS_PER_SECOND = 1000;
export const DELETION_GRACE_MS =
  DELETION_GRACE_DAYS * HOURS_PER_DAY * MINUTES_PER_HOUR * SECONDS_PER_MINUTE * MS_PER_SECOND;

export const useBusinessDeletionStore = create<BusinessDeletionState>()(
  persist(
    (set) => ({
      pending: null,
      setPending: (p) => set({ pending: p }),
      clear: () => set({ pending: null }),
    }),
    {
      name: 'onevoice.business-deletion',
      storage: createJSONStorage(() => localStorage),
    }
  )
);
