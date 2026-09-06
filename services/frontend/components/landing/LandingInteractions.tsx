'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import type { LandingEntryProps } from '@/lib/landing-entry';

const EVENTS_PATH = '/api/v1/landing-events';
const MOBILE_BAR_FOCUS_CLEARANCE = 96;

export function trackLandingClick(event: MouseEvent) {
  const link = event.target instanceof Element ? event.target.closest('a[data-cta]') : null;
  const cta = link?.getAttribute('data-cta');
  if (!cta) return;
  const body = JSON.stringify({ cta, path: window.location.pathname });
  try {
    if (navigator.sendBeacon?.(EVENTS_PATH, new Blob([body], { type: 'application/json' }))) return;
  } catch {}
  try {
    void fetch(EVENTS_PATH, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
      credentials: 'omit',
    }).catch(() => {});
  } catch {}
}

export function LandingInteractions({ mode }: LandingEntryProps) {
  const t = useTranslations('landing.nav');
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    document.addEventListener('click', trackLandingClick);
    function update() {
      const hero = document.getElementById('hero');
      const waitlist = document.getElementById('waitlist');
      const rect = waitlist?.getBoundingClientRect();
      const formVisible = rect && rect.top < window.innerHeight && rect.bottom > 0;
      const focused = document.activeElement;
      const editing =
        focused instanceof HTMLElement &&
        focused.matches('input, textarea, select, [role="combobox"]');
      const coveredFocus =
        focused instanceof HTMLElement &&
        focused !== document.body &&
        !focused.closest('[data-landing-bar]') &&
        focused.getBoundingClientRect().bottom > window.innerHeight - MOBILE_BAR_FOCUS_CLEARANCE;
      setVisible(
        Boolean(
          hero &&
          hero.getBoundingClientRect().bottom <= 0 &&
          !formVisible &&
          !editing &&
          !coveredFocus
        )
      );
    }
    const observer = new IntersectionObserver(update);
    for (const id of ['hero', 'waitlist']) {
      const element = document.getElementById(id);
      if (element) observer.observe(element);
    }
    window.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    update();
    return () => {
      document.removeEventListener('click', trackLandingClick);
      observer.disconnect();
      window.removeEventListener('scroll', update);
      window.removeEventListener('resize', update);
      document.removeEventListener('focusin', update);
      document.removeEventListener('focusout', update);
    };
  }, []);
  if (!visible) return null;
  return (
    <div
      data-landing-bar
      className="fixed inset-x-0 bottom-0 z-20 flex items-center justify-center gap-3 border-t border-line bg-paper px-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] pt-3 md:hidden"
    >
      <Button asChild variant="primary">
        <a
          href={mode === 'open' ? '/register' : '#waitlist'}
          data-cta={mode === 'open' ? 'mobile-register' : 'mobile-waitlist'}
        >
          {t(mode === 'open' ? 'start' : 'waitlist')}
        </a>
      </Button>
      <Button asChild variant="ghost">
        <a href="/login" data-cta="mobile-login">
          {t('login')}
        </a>
      </Button>
    </div>
  );
}
