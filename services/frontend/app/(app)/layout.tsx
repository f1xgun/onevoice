'use client'

import { useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { useAuthStore } from '@/lib/auth'
import { api } from '@/lib/api'
import { Sidebar } from '@/components/sidebar'
import type { ReactNode } from 'react'

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter()
  const { setAuth } = useAuthStore()
  // Start as true so we always show a loading state until the effect has run
  // This prevents the brief flash of protected content
  const [ready, setReady] = useState(false)
  const isMounted = useRef(true)

  useEffect(() => {
    isMounted.current = true

    const refreshToken = localStorage.getItem('refreshToken')

    if (!refreshToken) {
      router.replace('/login')
      return
    }

    const accessToken = useAuthStore.getState().accessToken
    if (accessToken) {
      // Already have a valid token in memory — show the page
      setReady(true)
      return
    }

    // Have a refresh token but no access token — attempt silent refresh
    api.post('/auth/refresh', { refreshToken })
      .then((res) => {
        if (isMounted.current) {
          setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
          setReady(true)
        }
      })
      .catch(() => {
        if (isMounted.current) {
          router.replace('/login')
        }
      })

    return () => { isMounted.current = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // mount-only: reads auth state once on load

  if (!ready) {
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
