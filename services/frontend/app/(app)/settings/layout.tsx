import type { ReactNode } from 'react';
import { getTranslator } from '@/lib/i18n/translator';
import { SettingsNavLink } from './_components/SettingsNavLink';

const tNav = getTranslator('settings.nav');

const TABS = [
  { href: '/settings', labelKey: 'profile' },
  { href: '/settings/tools', labelKey: 'tools' },
  { href: '/settings/team', labelKey: 'team' },
  { href: '/settings/roles', labelKey: 'roles' },
] as const;

export default function SettingsLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-col gap-6 md:flex-row md:gap-8">
      <nav
        aria-label={tNav('navAria')}
        className="flex w-full flex-row gap-1 overflow-x-auto px-4 pt-4 sm:px-12 md:w-48 md:flex-col md:overflow-visible md:px-4 md:pt-6"
      >
        {TABS.map((tab) => (
          <SettingsNavLink key={tab.href} href={tab.href} label={tNav(tab.labelKey)} />
        ))}
      </nav>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
