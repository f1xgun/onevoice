'use client';

import { useEffect, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';

import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';

// `/business/new` is the create-org page — reaching it with zero businesses
// is the legitimate entry from /onboarding, so we must not bounce back.
const BYPASS_PATHS = ['/login', '/register', '/onboarding', '/business/new'];

export function BusinessRequiredGuard({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname() ?? '';
  const t = useTranslations('businessGuard');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const setActive = useBusinessStore((s) => s.setActive);
  const { data: businesses, isLoading, error, refetch } = useBusinessList();
  const isBypass = BYPASS_PATHS.some((p) => pathname.startsWith(p));

  useEffect(() => {
    if (isBypass) return;
    if (!businesses) return;
    if (businesses.length === 0) {
      if (activeBusinessId !== null) {
        setActive(null);
      }
      router.replace('/onboarding');
      return;
    }
    const validIds = new Set(businesses.map((b) => b.id));
    if (!activeBusinessId || !validIds.has(activeBusinessId)) {
      setActive(businesses[0].id);
    }
  }, [businesses, activeBusinessId, setActive, router, isBypass]);

  if (isBypass) {
    return <>{children}</>;
  }

  if (error) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4">
        <p className="text-sm text-muted-foreground">{t('loadFailed')}</p>
        <button
          type="button"
          onClick={() => refetch()}
          className="hover:bg-primary/90 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
        >
          {t('retry')}
        </button>
      </div>
    );
  }

  if (isLoading || !activeBusinessId) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div
          role="status"
          aria-label={t('loading')}
          className="h-8 w-8 animate-spin rounded-full border-4 border-border border-t-transparent"
        />
      </div>
    );
  }

  return <>{children}</>;
}
