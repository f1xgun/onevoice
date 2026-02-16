import { describe, it, expect, beforeEach } from 'vitest'
import { useAuthStore } from '../auth'

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, accessToken: null })
    localStorage.clear()
  })

  it('sets user and token on login', () => {
    const user = { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' }
    useAuthStore.getState().setAuth(user, 'access-token', 'refresh-token')

    expect(useAuthStore.getState().user).toEqual(user)
    expect(useAuthStore.getState().accessToken).toBe('access-token')
    expect(useAuthStore.getState().isAuthenticated).toBe(true)
    expect(localStorage.getItem('refreshToken')).toBe('refresh-token')
  })

  it('clears state on logout', () => {
    useAuthStore.getState().setAuth(
      { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' },
      'access-token',
      'refresh-token'
    )
    useAuthStore.getState().logout()

    expect(useAuthStore.getState().user).toBeNull()
    expect(useAuthStore.getState().accessToken).toBeNull()
    expect(useAuthStore.getState().isAuthenticated).toBe(false)
    expect(localStorage.getItem('refreshToken')).toBeNull()
  })
})
