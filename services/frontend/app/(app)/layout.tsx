'use client';

import { useEffect, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
import { loginRedirectPath } from '@/lib/postAuthRedirect';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { refreshAccessToken } from '@/lib/api/authFetch';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { trackEvent } from '@/lib/telemetry';
import { useIsDesktop } from '@/hooks/useIsDesktop';
import { Sidebar } from '@/components/sidebar';
import { NavRail } from '@/components/sidebar/NavRail';
import { ProjectPane } from '@/components/sidebar/ProjectPane';
import { BusinessRequiredGuard } from '@/components/BusinessRequiredGuard';
import { PermissionsCacheGuard } from '@/components/PermissionsCacheGuard';
// persistent yellow banner when emailVerified===false.
// Mounted inside the <main> scroll container so it cohabits with route
// content (sticky top-0 keeps it visible while the page scrolls).
import { VerificationBanner } from '@/components/auth/VerificationBanner';
// persistent RED banner when accountDeletion !== null.
// UI-SPEC Surface 10: outranks VerificationBanner — mounts ABOVE so the
// deletion-grace state visually wins when both could fire (the user
// can be both unverified AND mid-grace).
import { DeletionGraceBanner } from '@/components/account/DeletionGraceBanner';
// forced re-consent modal — z-50 portal that
// renders OUTSIDE <main> when the user has stale policy versions and
// is past the email-verification gate. Mutually exclusive with
// EmailVerifiedRequiredModal per the modal-precedence rule (UI-SPEC
// §Cross-surface stacking).
import { ReConsentModal } from '@/components/legal/ReConsentModal';
import type { ReactNode } from 'react';

// Module-level event-name singleton: any input/element listening for this
// CustomEvent will focus itself. Decouples the layout (broadcaster) from
// the SearchBar (consumer) — consumers can mount/unmount as the route
// changes without re-binding the global keyboard listener.
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const tSidebar = useTranslations('sidebar');
  const tSession = useTranslations('auth.session');
  const { setAuth } = useAuthStore();
  const [ready, setReady] = useState(false);
  const [sessionError, setSessionError] = useState(false);
  const [sessionAttempt, setSessionAttempt] = useState(0);
  const isDesktop = useIsDesktop();

  useEffect(() => {
    const controller = new AbortController();

    setSessionError(false);
    const accessToken = useAuthStore.getState().accessToken;
    if (accessToken && sessionAttempt === 0) {
      setReady(true);
      api
        .get(API_PATHS.AUTH.ME, { signal: controller.signal })
        .then((res) => {
          if (controller.signal.aborted) return;
          setAuth(res.data, useAuthStore.getState().accessToken!);
        })
        .catch(() => {});
    } else {
      async function restoreSession() {
        try {
          if (!accessToken) await refreshAccessToken();
        } catch (err) {
          if (controller.signal.aborted) return;
          const status = (err as { response?: { status?: number } })?.response?.status;
          if (status === HTTP_STATUS.UNAUTHORIZED) {
            router.replace(loginRedirectPath(window.location));
          } else {
            setSessionError(true);
          }
          return;
        }
        if (controller.signal.aborted) return;
        try {
          const res = await api.get(API_PATHS.AUTH.ME, { signal: controller.signal });
          if (controller.signal.aborted) return;
          setAuth(res.data, useAuthStore.getState().accessToken!);
          setReady(true);
        } catch {
          if (!controller.signal.aborted) setSessionError(true);
        }
      }
      void restoreSession();
    }

    return () => {
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionAttempt]);

  useEffect(() => {
    if (ready) {
      trackEvent('page_view', pathname, { page: pathname });
    }
  }, [pathname, ready]);

  useEffect(() => {
    function onKeydown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT));
      }
    }
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  }, []);

  if (sessionError) {
    return (
      <div className="flex h-dvh flex-col items-center justify-center gap-4 px-6 text-center">
        <p className="text-sm text-ink-mid">{tSession('refreshFailed')}</p>
        <Button type="button" onClick={() => setSessionAttempt((n) => n + 1)}>
          {tSession('retry')}
        </Button>
      </div>
    );
  }

  if (!ready) {
    return null;
  }

  const showProjectPane = pathname.startsWith('/chat/') || pathname.startsWith('/projects');

  const user = useAuthStore.getState().user;
  const requiresEmailVerification =
    user?.requiresEmailVerification === true || user?.emailVerified === false;
  const showReConsent = !!user?.requiresReconsent && !requiresEmailVerification;

  return (
    <BusinessRequiredGuard>
      <>
        <PermissionsCacheGuard />
        {isDesktop ? (
          <div className="flex h-dvh">
            <NavRail />
            <PanelGroup
              direction="horizontal"
              autoSaveId="onevoice:sidebar-width"
              className="flex-1"
            >
              {showProjectPane && (
                <>
                  <Panel
                    id="project-pane"
                    order={1}
                    defaultSize={22}
                    minSize={12}
                    maxSize={35}
                    className="motion-reduce:transition-none"
                  >
                    <ProjectPane />
                  </Panel>
                  <PanelResizeHandle
                    id="project-pane-handle"
                    aria-label={tSidebar('resizeAria')}
                    aria-orientation="vertical"
                    className="h-full w-px bg-[var(--ov-line)] transition-colors hover:bg-[var(--ov-ink-faint)]"
                  />
                </>
              )}
              <Panel id="main" order={2} defaultSize={78} className="motion-reduce:transition-none">
                <main
                  id="main-content"
                  tabIndex={-1}
                  className="flex h-full scroll-pb-28 scroll-pt-20 flex-col overflow-y-auto bg-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink md:scroll-py-8"
                >
                  <DeletionGraceBanner />
                  <VerificationBanner />
                  <div className="min-h-0 flex-1">{children}</div>
                </main>
              </Panel>
            </PanelGroup>
          </div>
        ) : (
          <div className="flex h-dvh flex-col">
            <Sidebar />
            <main
              id="main-content"
              tabIndex={-1}
              className="flex min-h-0 flex-1 scroll-pb-28 scroll-pt-20 flex-col overflow-y-auto bg-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink md:scroll-py-8"
            >
              <DeletionGraceBanner />
              <VerificationBanner />
              <div className="min-h-0 flex-1">{children}</div>
            </main>
          </div>
        )}
        {showReConsent && user?.requiresReconsent && (
          <ReConsentModal policies={user.requiresReconsent.policies} />
        )}
      </>
    </BusinessRequiredGuard>
  );
}
