'use client'

import { useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { useAuthStore } from '@/lib/auth'
import { api } from '@/lib/api'
import { Sidebar } from '@/components/sidebar'
import type { ReactNode } from 'react'

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter()
  const { isAuthenticated, setAuth } = useAuthStore()
  const [checking, setChecking] = useState(false)
  const isMounted = useRef(true)

  useEffect(() => {
    isMounted.current = true
    const refreshToken = typeof window !== 'undefined' ? localStorage.getItem('refreshToken') : null

    if (!refreshToken) {
      router.replace('/login')
      return
    }

    const accessToken = useAuthStore.getState().accessToken
    if (!accessToken) {
      setChecking(true)
      api.post('/auth/refresh', { refreshToken })
        .then((res) => {
          if (isMounted.current) {
            setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
          }
        })
        .catch(() => {
          if (isMounted.current) {
            router.replace('/login')
          }
        })
        .finally(() => {
          if (isMounted.current) setChecking(false)
        })
    }

    return () => { isMounted.current = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // mount-only: intentionally reads store state at mount time

  if (checking) {
    return null
  }

  if (!isAuthenticated && typeof window !== 'undefined' && !localStorage.getItem('refreshToken')) {
    return null
  }

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-y-auto bg-gray-50">
        {children}
      </main>
    </div>
  )
}
