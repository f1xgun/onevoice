'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

export interface BusinessState {
  activeBusinessId: string | null;
  userId: string | null;
  reconcileUser: (userId: string) => void;
  setActive: (id: string | null) => void;
  clear: () => void;
}

export const useBusinessStore = create<BusinessState>()(
  persist(
    (set) => ({
      activeBusinessId: null,
      userId: null,
      reconcileUser: (userId) =>
        set((state) => ({
          userId,
          activeBusinessId: state.userId === userId ? state.activeBusinessId : null,
        })),
      setActive: (id) => set({ activeBusinessId: id }),
      clear: () => set({ activeBusinessId: null, userId: null }),
    }),
    {
      name: 'onevoice.business',
      storage: createJSONStorage(() => localStorage),
    }
  )
);
