'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

/** Records a just-deleted organization until the server list refreshes. */
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
