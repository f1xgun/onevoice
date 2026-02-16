import axios from 'axios'
import { useAuthStore } from './auth'

export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
})

// Attach access token to every request
api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// On 401: try refresh once, then logout
let refreshing = false
let queue: Array<{ resolve: (v: string) => void; reject: (e: unknown) => void }> = []

api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const original = error.config

    if (error.response?.status !== 401 || original._retry) {
      return Promise.reject(error)
    }

    if (refreshing) {
      return new Promise((resolve, reject) => {
        queue.push({ resolve, reject })
      }).then((token) => {
        original.headers.Authorization = `Bearer ${token}`
        return api(original)
      })
    }

    original._retry = true
    refreshing = true

    try {
      const refreshToken = localStorage.getItem('refreshToken')
      if (!refreshToken) throw new Error('no refresh token')

      interface RefreshResponse {
        accessToken: string
        refreshToken: string
      }
      const { data } = await axios.post<RefreshResponse>('/api/v1/auth/refresh', { refreshToken })
      if (!data.accessToken) throw new Error('invalid refresh response')
      const { accessToken, refreshToken: newRefresh } = data

      useAuthStore.getState().setAccessToken(accessToken)
      localStorage.setItem('refreshToken', newRefresh)

      queue.forEach(({ resolve }) => resolve(accessToken))
      queue = []

      original.headers.Authorization = `Bearer ${accessToken}`
      return api(original)
    } catch {
      queue.forEach(({ reject }) => reject(error))
      queue = []
      useAuthStore.getState().logout()
      window.location.href = '/login'
      return Promise.reject(error)
    } finally {
      refreshing = false
    }
  }
)
