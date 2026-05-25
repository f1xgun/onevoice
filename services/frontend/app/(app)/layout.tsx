'use client';

import { useEffect, useRef, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';
import { trackEvent } from '@/lib/telemetry';
import { useIsDesktop } from '@/hooks/useIsDesktop';
import { Sidebar } from '@/components/sidebar';
import { NavRail } from '@/components/sidebar/NavRail';
import { ProjectPane } from '@/components/sidebar/ProjectPane';
import { BusinessRequiredGuard } from '@/components/BusinessRequiredGuard';
import { PermissionsCacheGuard } from '@/components/PermissionsCacheGuard';
// Phase 21-03 (ACCT-02): persistent yellow banner when emailVerified===false.
// Mounted inside the <main> scroll container so it cohabits with route
// content (sticky top-0 keeps it visible while the page scrolls).
import { VerificationBanner } from '@/components/auth/VerificationBanner';
// Phase 21-04 (ACCT-03): persistent RED banner when accountDeletion !== null.
// UI-SPEC Surface 10: outranks VerificationBanner — mounts ABOVE so the
// deletion-grace state visually wins when both could fire (the user
// can be both unverified AND mid-grace).
import { DeletionGraceBanner } from '@/components/account/DeletionGraceBanner';
// Phase 22-02 (D-25/D-26): site-wide Footer mounted at the bottom of
// BOTH desktop and mobile <main> branches (Phase 21 verification GAP
// taught us: forget the mobile branch and the banner / footer never
// renders on phones).
import { Footer } from '@/components/layout/Footer';
// Phase 22-02 (D-11): forced re-consent modal — z-50 portal that
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
  const { setAuth } = useAuthStore();
  // Start as true so we always show a loading state until the effect has run
  // This prevents the brief flash of protected content
  const [ready, setReady] = useState(false);
  const isDesktop = useIsDesktop();
  const isMounted = useRef(true);

  useEffect(() => {
    isMounted.current = true;
    const controller = new AbortController();

    const accessToken = useAuthStore.getState().accessToken;
    if (accessToken) {
      // Already have a valid token in memory — show the page
      setReady(true);
      return;
    }

    // No access token in memory — attempt silent refresh via httpOnly cookie
    api
      .post('/auth/refresh', {}, { signal: controller.signal })
      .then((res) => {
        if (!isMounted.current) return;
        useAuthStore.getState().setAccessToken(res.data.accessToken);
        return api.get(API_PATHS.AUTH.ME, { signal: controller.signal });
      })
      .then((res) => {
        if (!isMounted.current || !res) return;
        setAuth(res.data, useAuthStore.getState().accessToken!);
        setReady(true);
      })
      .catch((_err: unknown) => {
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

  useEffect(() => {
    if (ready) {
      trackEvent('page_view', pathname, { page: pathname });
    }
  }, [pathname, ready]);

  // Cmd/Ctrl-K global focus listener. Steals focus from any input
  // INCLUDING the chat composer — Slack/Linear convention. Mount-only.
  useEffect(() => {
    function onKeydown(e: KeyboardEvent) {
      // metaKey covers Cmd on macOS; ctrlKey covers Ctrl on every other
      // platform. Match `K`/`k` — different keyboard layouts may emit either.
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT));
      }
    }
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  }, []);

  if (!ready) {
    return null;
  }

  // project-pane is route-conditional. Rendered on /chat/* and
  // /projects/* only. Other authenticated routes show NavRail + content.
  const showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects');

  // Phase 22-02 (D-11): ReConsentModal rendering condition. The
  // EmailVerifiedRequiredModal (Phase 21-03) still takes precedence —
  // we must NOT render both. We read the user's email-verification
  // state from emailVerified===false combined with the active
  // verification deadline (existing convention; the explicit
  // requiresEmailVerification flag is forward-compat).
  const user = useAuthStore.getState().user;
  const requiresEmailVerification =
    user?.requiresEmailVerification === true || user?.emailVerified === false;
  const showReConsent = !!user?.requiresReconsent && !requiresEmailVerification;

  return (
    <BusinessRequiredGuard>
      <>
        {/* Phase 5 RBAC: invalidate per-business permissions cache on every
            business switch. Mounted here so it's active for the entire (app)
            shell but only after BusinessRequiredGuard resolves a valid
            activeBusinessId. Renders no DOM. */}
        <PermissionsCacheGuard />
        {isDesktop ? (
          <div className="flex h-screen">
            <NavRail />
            <PanelGroup
              direction="horizontal"
              autoSaveId="onevoice:sidebar-width"
              className="flex-1"
            >
              {showProjectPane && (
                <>
                  {/* defaultSize=22 ≈ 280 px on a 1280 px viewport
                      (default 280 px). minSize=12 / maxSize=35 cover the
                      locked 200–480 px range without clipping.
                      Explicit id+order keep the panel registry stable when
                      showProjectPane toggles between routes — without them
                      react-resizable-panels v3 re-keys panels on remount and
                      the resize handle ends up reporting deltas against the
                      wrong neighbour, inverting the drag direction. */}
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
                  {/* PanelResizeHandle already renders role="separator" with
                      aria-controls + aria-label, so it's a self-contained,
                      focusable, labelled control. The earlier role="region"
                      wrapper duplicated the announcement (axe M4 review):
                      Tab would announce "region, Изменить ширину…" then
                      "separator, Изменить ширину…". Drop the wrapper.
                      aria-orientation="vertical" disambiguates the separator
                      axis for AT and documents intent for axe's region rule —
                      a 1-px separator between two landmarks is not itself a
                      landmark, so the residual region-rule warning is a known
                      false positive. */}
                  <PanelResizeHandle
                    id="project-pane-handle"
                    aria-label={tSidebar('resizeAria')}
                    aria-orientation="vertical"
                    className="h-full w-px bg-[var(--ov-line)] transition-colors hover:bg-[var(--ov-ink-faint)]"
                  />
                </>
              )}
              <Panel id="main" order={2} defaultSize={78} className="motion-reduce:transition-none">
                {/* tabIndex={-1}: makes <main> programmatically focusable so
                    activating the SkipLink moves keyboard focus here. Without
                    it, the hash navigation only scrolls; the next Tab would
                    advance from the link's DOM position (back into the
                    sidebar). Doesn't add <main> to the tab order.
                    focus-visible:outline-ink (keyboard-only, not mouse) gives
                    a brief visible confirmation that focus actually moved
                    here — required by WCAG 2.4.7 since the skip-link's only
                    purpose is to transfer focus. */}
                <main
                  id="main-content"
                  tabIndex={-1}
                  className="flex h-full flex-col overflow-y-auto bg-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
                >
                  <DeletionGraceBanner />
                  <VerificationBanner />
                  <div className="flex-1">{children}</div>
                  <Footer />
                </main>
              </Panel>
            </PanelGroup>
          </div>
        ) : (
          // h-screen + flex-col gives <main> a real height — without it h-full
          // children (chat composer) collapse to 0 and drift to content end.
          <div className="flex h-screen flex-col">
            <Sidebar />
            {/* tabIndex={-1}: see desktop branch above — required for the
                SkipLink to actually transfer keyboard focus into <main>.
                focus-visible outline mirrors the desktop branch — a brief
                ink ring is the only cue that focus moved (WCAG 2.4.7). */}
            <main
              id="main-content"
              tabIndex={-1}
              className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
            >
              <DeletionGraceBanner />
              <VerificationBanner />
              <div className="flex-1">{children}</div>
              <Footer />
            </main>
          </div>
        )}
        {/* Phase 22-02 Surface E (D-11): forced re-consent. Portal-mounted
            at the top level so the modal sits OUTSIDE <main> and reaches
            z-50 above any banners. Mutually exclusive with the email-
            verification modal — that one still wins (UI-SPEC §Modal
            precedence rule). */}
        {showReConsent && user?.requiresReconsent && (
          <ReConsentModal policies={user.requiresReconsent.policies} />
        )}
      </>
    </BusinessRequiredGuard>
  );
}
