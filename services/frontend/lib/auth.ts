import { create } from 'zustand'

export type UserRole = 'owner' | 'admin' | 'member'

export interface User {
  id: string
  email: string
  name: string
  role: UserRole
}

interface AuthState {
  user: User | null
  accessToken: string | null
  isAuthenticated: boolean
  setAuth: (user: User, accessToken: string, refreshToken: string) => void
  setAccessToken: (token: string) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  accessToken: null,
  isAuthenticated: false,

  setAuth: (user, accessToken, refreshToken) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('refreshToken', refreshToken)
    }
    set({ user, accessToken, isAuthenticated: true })
  },

  setAccessToken: (token) => {
    set({ accessToken: token, isAuthenticated: !!token })
  },

  logout: () => {
    if (typeof window !== 'undefined') {
      localStorage.removeItem('refreshToken')
    }
    set({ user: null, accessToken: null, isAuthenticated: false })
  },
}))
