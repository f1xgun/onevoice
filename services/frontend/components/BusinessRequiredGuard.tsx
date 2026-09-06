'use client';

import { useEffect, type ReactNode } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';

import { useBusinessStore } from '@/lib/stores/business';
import { BusinessDeletionGraceBanner } from '@/components/business/BusinessDeletionGraceBanner';
import { useBusinessList } from '@/lib/hooks/useBusinessList';

const OWNER_ROLE_ID = '00000000-0000-0000-0000-000000000001';

// `/business/new` is the create-org page — reaching it with zero businesses
// is the legitimate entry from /onboarding, so we must not bounce back.
const BYPASS_PATHS = ['/login', '/register', '/onboarding', '/business/new'];

// Routes scoped to the person rather than to an organization: the profile +
// password form, the account-deletion danger zone and the consent-withdrawal
// panel. Somebody who has no organization (just registered, or left their
// last one) must still be able to reach them — otherwise the only way out of
// the product is clearing cookies. Matched exactly, so the organization tabs
// under /settings (team, roles, billing, audit, tools) stay gated.
const ACCOUNT_SCOPED_PATHS = ['/settings', '/settings/account', '/settings/privacy'];

function normalizePath(pathname: string): string {
  return pathname.length > 1 && pathname.endsWith('/') ? pathname.slice(0, -1) : pathname;
}

export function BusinessRequiredGuard({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname() ?? '';
  const t = useTranslations('businessGuard');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const setActive = useBusinessStore((s) => s.setActive);
  const { data: businesses, isLoading, error, refetch } = useBusinessList();
  const isBypass = BYPASS_PATHS.some((p) => pathname.startsWith(p));
  const isAccountScoped = ACCOUNT_SCOPED_PATHS.includes(normalizePath(pathname));
  const hasNoBusiness = businesses?.length === 0;

  useEffect(() => {
    if (isBypass) return;
    if (!businesses) return;
    if (businesses.length === 0) {
      if (activeBusinessId !== null) {
        setActive(null);
      }
      if (!isAccountScoped) {
        router.replace('/onboarding');
      }
      return;
    }
    const available = businesses.filter((b) => !b.deletion_pending_until);
    const validIds = new Set(available.map((b) => b.id));
    if (!activeBusinessId || !validIds.has(activeBusinessId)) {
      setActive(available[0]?.id ?? null);
    }
  }, [businesses, activeBusinessId, setActive, router, isBypass, isAccountScoped]);

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
          className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-brand-hover"
        >
          {t('retry')}
        </button>
      </div>
    );
  }

  const pending = businesses?.filter((b) => b.deletion_pending_until) ?? [];
  const restoration = pending.map((b) =>
    b.role.id === OWNER_ROLE_ID ? (
      <BusinessDeletionGraceBanner
        key={b.id}
        pendingDeletion={{
          id: b.id,
          name: b.name,
          scheduledDeletionAt: b.deletion_pending_until!,
        }}
        onRestored={refetch}
      />
    ) : (
      <p key={b.id} role="status" className="p-4 text-sm text-muted-foreground">
        {t('pendingOwnerRestore', { name: b.name })}
      </p>
    )
  );

  if (!isLoading && businesses?.length && pending.length === businesses.length) {
    return (
      <>
        {restoration}
        {isAccountScoped && children}
      </>
    );
  }

  const activeIsAvailable = businesses?.some(
    (b) => b.id === activeBusinessId && !b.deletion_pending_until
  );
  if (
    isLoading ||
    (!activeBusinessId && !(isAccountScoped && hasNoBusiness)) ||
    (!!activeBusinessId && !activeIsAvailable)
  ) {
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

  return (
    <>
      {restoration}
      {children}
    </>
  );
}
