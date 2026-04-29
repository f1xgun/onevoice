'use client';

import { useEffect, useRef, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
import { useAuthStore } from '@/lib/auth';
import { api } from '@/lib/api';
import { trackEvent } from '@/lib/telemetry';
import { Sidebar } from '@/components/sidebar';
import { NavRail } from '@/components/sidebar/NavRail';
import { ProjectPane } from '@/components/sidebar/ProjectPane';
import type { ReactNode } from 'react';

// Module-level event-name singleton: any input/element listening for this
// CustomEvent will focus itself. Decouples the layout (broadcaster) from
// the SearchBar (consumer) — consumers can mount/unmount as the route
// changes without re-binding the global keyboard listener.
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { setAuth } = useAuthStore();
  // Start as true so we always show a loading state until the effect has run
  // This prevents the brief flash of protected content
  const [ready, setReady] = useState(false);
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
        return api.get('/auth/me', { signal: controller.signal });
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

  // D-11: Cmd/Ctrl-K global focus listener. Steals focus from any input
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

  // D-14: project-pane is route-conditional. Rendered on /chat/* and
  // /projects/* only. Other authenticated routes show NavRail + content.
  const showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects');

  return (
    <>
      {/* Mobile: keep the existing Sheet-based drawer (Sidebar) which
          renders top bar + drawer with the full nav + project tree.
          The flex-column + h-screen pair gives <main> a real height so
          h-full layouts inside (chat composer at the bottom, etc.)
          actually work — without it h-full collapses to 0 and the
          composer drifts to wherever content ends. */}
      <div className="flex h-screen flex-col md:hidden">
        <Sidebar />
        <main className="min-h-0 flex-1 overflow-y-auto bg-background">{children}</main>
      </div>

      {/* Desktop: NavRail (always) + PanelGroup hosting conditional
          ProjectPane and main content. autoSaveId persists the resized
          width to localStorage under
          `react-resizable-panels:onevoice:sidebar-width` (D-15). */}
      <div className="hidden h-screen md:flex">
        <NavRail />
        <PanelGroup direction="horizontal" autoSaveId="onevoice:sidebar-width" className="flex-1">
          {showProjectPane && (
            <>
              {/* defaultSize=22 ≈ 280 px on a 1280 px viewport (D-15
                  default 280 px). minSize=12 / maxSize=35 cover the
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
              <PanelResizeHandle
                id="project-pane-handle"
                aria-label="Изменить ширину боковой панели"
                className="w-px bg-[var(--ov-line)] transition-colors hover:bg-[var(--ov-ink-faint)]"
              />
            </>
          )}
          <Panel id="main" order={2} defaultSize={78} className="motion-reduce:transition-none">
            <main className="h-full overflow-y-auto bg-background">{children}</main>
          </Panel>
        </PanelGroup>
      </div>
    </>
  );
}
