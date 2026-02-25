'use client';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { Sidebar } from '@/components/sidebar';
import type { ReactNode } from 'react';

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter();
  const { setAuth } = useAuthStore();
  // Start as true so we always show a loading state until the effect has run
  // This prevents the brief flash of protected content
  const [ready, setReady] = useState(false);
  const isMounted = useRef(true);

  useEffect(() => {
    isMounted.current = true;
    const controller = new AbortController();

    const refreshToken = localStorage.getItem('refreshToken');

    if (!refreshToken) {
      router.replace('/login');
      return;
    }

    const accessToken = useAuthStore.getState().accessToken;
    if (accessToken) {
      // Already have a valid token in memory — show the page
      setReady(true);
      return;
    }

    // Have a refresh token but no access token — attempt silent refresh
    let newRefreshToken = refreshToken;

    api
      .post('/auth/refresh', { refreshToken }, { signal: controller.signal })
      .then((res) => {
        if (!isMounted.current) return;
        // Capture the new refresh token from rotation before it's lost
        newRefreshToken = res.data.refreshToken ?? refreshToken;
        useAuthStore.getState().setAccessToken(res.data.accessToken);
        return api.get('/auth/me', { signal: controller.signal });
      })
      .then((res) => {
        if (!isMounted.current || !res) return;
        setAuth(res.data, useAuthStore.getState().accessToken!, newRefreshToken);
        setReady(true);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        if (isMounted.current) {
          router.replace('/login');
        }
      });

    return () => {
      isMounted.current = false;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // mount-only: reads auth state once on load

  if (!ready) {
    return null;
  }

  return (
    <div className="flex h-screen flex-col overflow-hidden md:flex-row">
      <Sidebar />
      <main className="flex-1 overflow-y-auto bg-gray-50">{children}</main>
    </div>
  );
}
