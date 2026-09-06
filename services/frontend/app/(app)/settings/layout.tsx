import type { ReactNode } from 'react';
import { getTranslations } from 'next-intl/server';
import { SettingsNavLink } from './_components/SettingsNavLink';

const TABS = [
  { href: '/settings', labelKey: 'profile' },
  { href: '/settings/tools', labelKey: 'tools' },
  { href: '/settings/team', labelKey: 'team' },
  { href: '/settings/roles', labelKey: 'roles' },
  { href: '/settings/billing', labelKey: 'billing', perm: 'billing.read' },
  { href: '/settings/audit', labelKey: 'audit' },
  { href: '/settings/privacy', labelKey: 'privacy' },
  { href: '/settings/account', labelKey: 'account' },
] as const;

// Server component (no 'use client' directive) — pulls request-scoped
// translations via getTranslations from next-intl/server so a locale
// switch is reflected on the next render without needing client-state.
export default async function SettingsLayout({ children }: { children: ReactNode }) {
  const tNav = await getTranslations('settings.nav');
  return (
    <div className="flex flex-col gap-6 md:flex-row md:gap-8">
      <nav
        aria-label={tNav('navAria')}
        className="flex w-full flex-row gap-1 overflow-x-auto px-4 pt-4 sm:px-12 md:w-48 md:flex-col md:overflow-visible md:px-4 md:pt-6"
      >
        {TABS.map((tab) => (
          <SettingsNavLink
            key={tab.href}
            href={tab.href}
            label={tNav(tab.labelKey)}
            perm={'perm' in tab ? tab.perm : undefined}
          />
        ))}
      </nav>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
