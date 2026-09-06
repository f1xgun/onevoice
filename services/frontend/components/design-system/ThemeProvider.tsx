'use client';

import { createContext, useContext } from 'react';
import type { ReactNode } from 'react';
import { DEFAULT_THEME } from '@/lib/theme';
import type { Theme } from '@/lib/theme';

const ThemeContext = createContext<Theme>(DEFAULT_THEME);

interface ThemeProviderProps {
  theme: Theme;
  children: ReactNode;
}

export function ThemeProvider({ theme, children }: ThemeProviderProps) {
  return <ThemeContext.Provider value={theme}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  return useContext(ThemeContext);
}
