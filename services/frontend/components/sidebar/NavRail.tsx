'use client';

import { useState } from 'react';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { AppearanceControls } from '@/components/design-system/AppearanceControls';
import { FeedbackDialog } from '@/components/feedback/FeedbackDialog';
import { BusinessSwitcher } from '@/components/business-switcher/BusinessSwitcher';
import { useAuthStore } from '@/lib/auth';
import { usePermission } from '@/lib/hooks/usePermission';
import { useLogout } from '@/lib/hooks/useLogout';
import { useDecisionQueue } from '@/hooks/useDecisionQueue';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { useBusinessStore } from '@/lib/stores/business';
import { cn } from '@/lib/utils';

const navItems = [
  { href: '/inbox', labelKey: 'waiting' },
  { href: '/chat', labelKey: 'instructions' },
  { href: '/posts', labelKey: 'posts' },
  { href: '/reviews', labelKey: 'reviews' },
  { href: '/business', labelKey: 'location' },
  { href: '/integrations', labelKey: 'platforms' },
  { href: '/settings', labelKey: 'settings' },
];
const settingsItems = [
  { href: '/settings/rules', labelKey: 'rules', matches: ['/settings/rules', '/settings/tools'] },
  { href: '/settings/team', labelKey: 'team', matches: ['/settings/team', '/settings/roles'] },
  { href: '/settings/billing', labelKey: 'plan', matches: ['/settings/billing'] },
  { href: '/settings/account', labelKey: 'account', matches: ['/settings/account'] },
  {
    href: '/settings/data',
    labelKey: 'data',
    matches: ['/settings/data', '/settings/privacy', '/settings/audit'],
  },
];

export interface NavRailProps {
  onNavigate?: () => void;
  expanded?: boolean;
}

export function NavRail({ onNavigate, expanded = false }: NavRailProps = {}) {
  const pathname = usePathname();
  const decisions = useDecisionQueue();
  const router = useRouter();
  const tNav = useTranslations('nav');
  const tSidebar = useTranslations('sidebar');
  const tRoles = useTranslations('team.roles');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const businesses = useBusinessList();
  const role = businesses.data?.find((business) => business.id === activeBusinessId)?.role.name;
  const roleLabel =
    role === 'admin' ? tNav('administrator') : role && tRoles.has(role) ? tRoles(role) : role;
  const user = useAuthStore((s) => s.user);
  const { allowed: canReadBilling } = usePermission('billing.read');
  const logout = useLogout();
  const [feedbackOpen, setFeedbackOpen] = useState(false);
  const inSettings = pathname.startsWith('/settings');

  async function handleLogout() {
    await logout();
    router.push('/login');
  }

  return (
    <aside
      data-testid="nav-rail"
      data-ov-motion
      aria-label={tSidebar('railWrapperAria')}
      className={cn(
        'flex shrink-0 flex-col bg-background px-4 pb-5 pt-5 text-ink',
        expanded ? 'min-h-full w-full gap-2 px-3 pt-0' : 'h-dvh w-[232px] overflow-y-auto'
      )}
    >
      <div className={expanded ? 'pr-10 [&>button]:mb-0 [&>button]:px-0' : undefined}>
        <BusinessSwitcher />
      </div>
      <nav
        aria-label={tNav('railAria')}
        className={cn('flex flex-col gap-0.5', expanded && 'rounded-block bg-paper-raised p-1')}
      >
        {navItems.map(({ href, labelKey }) => {
          const active = pathname === href || pathname.startsWith(`${href}/`);
          return (
            <div key={href}>
              <Link
                href={href}
                aria-current={
                  active && (!inSettings || pathname === '/settings') ? 'page' : undefined
                }
                onClick={onNavigate}
                className={cn(
                  'flex min-h-10 items-center rounded-inner px-2.5 py-2 text-[15px] leading-5 transition-colors hover:bg-paper-raised',
                  expanded && 'min-h-12 px-3 text-base',
                  active &&
                    (!inSettings || pathname === '/settings') &&
                    'bg-paper-raised font-medium'
                )}
              >
                {tNav(labelKey)}
                {labelKey === 'waiting' && decisions.data && decisions.data.total > 0 && (
                  <span className="ml-auto rounded-full bg-count px-1.5 text-meta text-count-foreground">
                    {decisions.data.total}
                  </span>
                )}
              </Link>
              {href === '/settings' && inSettings && (
                <div className="mt-1 flex flex-col gap-0.5" aria-label={tNav('settings')}>
                  {settingsItems.map((item) => {
                    if (item.labelKey === 'plan' && !canReadBilling) return null;
                    const selected = item.matches.some(
                      (href) =>
                        pathname === href ||
                        (href !== '/settings' && pathname.startsWith(`${href}/`))
                    );
                    return (
                      <Link
                        key={item.labelKey}
                        href={item.href}
                        aria-current={selected ? 'page' : undefined}
                        onClick={onNavigate}
                        className={cn(
                          'flex min-h-10 items-center rounded-inner py-2 pl-6 pr-2 text-[15px] leading-5 hover:bg-paper-raised',
                          expanded && 'min-h-12 text-base',
                          selected && 'bg-paper-raised font-medium'
                        )}
                      >
                        {tNav(item.labelKey)}
                      </Link>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </nav>
      <details className="mt-auto pt-7" open={expanded || undefined}>
        <summary className="cursor-pointer list-none rounded-inner px-2.5 py-2 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand">
          <span className="block break-words font-semibold">{user?.name || tNav('account')}</span>
          {roleLabel && <span className="block text-ink-soft">{roleLabel}</span>}
          {expanded && <span className="block break-words text-ink-soft">{user?.email}</span>}
        </summary>
        <div className={cn('mt-2 rounded-block bg-paper-raised p-1', expanded && 'p-1')}>
          <AppearanceControls variant="navigation" />
          <button
            type="button"
            onClick={() => setFeedbackOpen(true)}
            className="min-h-11 w-full rounded-inner px-3 text-left text-sm hover:bg-paper-sunken"
          >
            {tNav('feedback')}
          </button>
          <button
            type="button"
            onClick={handleLogout}
            className="min-h-11 w-full rounded-inner px-3 text-left text-sm hover:bg-paper-sunken"
          >
            {tNav('logout')}
          </button>
        </div>
      </details>
      <FeedbackDialog open={feedbackOpen} onOpenChange={setFeedbackOpen} />
    </aside>
  );
}
