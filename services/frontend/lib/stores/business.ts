'use client';

import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

export interface BusinessState {
  activeBusinessId: string | null;
  setActive: (id: string | null) => void;
  clear: () => void;
}

export const useBusinessStore = create<BusinessState>()(
  persist(
    (set) => ({
      activeBusinessId: null,
      setActive: (id) => set({ activeBusinessId: id }),
      clear: () => set({ activeBusinessId: null }),
    }),
    {
      name: 'onevoice.business',
      storage: createJSONStorage(() => localStorage),
    }
  )
);
