'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { useAuthStore } from '@/lib/auth'
import { api } from '@/lib/api'
import { Sidebar } from '@/components/sidebar'
import type { ReactNode } from 'react'

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter()
  const { isAuthenticated, setAuth, accessToken } = useAuthStore()
  const [checking, setChecking] = useState(true)

  useEffect(() => {
    const refreshToken = typeof window !== 'undefined' ? localStorage.getItem('refreshToken') : null

    if (!refreshToken) {
      router.replace('/login')
      setChecking(false)
      return
    }

    if (!accessToken) {
      api.post('/auth/refresh', { refreshToken })
        .then((res) => {
          setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
        })
        .catch(() => {
          router.replace('/login')
        })
        .finally(() => setChecking(false))
    } else {
      setChecking(false)
    }
  }, [])

  if (checking && !isAuthenticated) {
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
