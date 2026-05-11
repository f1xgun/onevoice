'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { cn } from '@/lib/utils';

export interface SettingsNavLinkProps {
  href: string;
  label: string;
}

export function SettingsNavLink({ href, label }: SettingsNavLinkProps) {
  const pathname = usePathname();
  const isActive = pathname === href;
  return (
    <Link
      href={href}
      aria-current={isActive ? 'page' : undefined}
      className={cn(
        'relative flex h-10 items-center rounded-md px-3 text-sm transition-colors',
        isActive ? 'text-ink' : 'text-ink-soft hover:bg-paper-sunken hover:text-ink'
      )}
    >
      {isActive && (
        <span aria-hidden className="absolute -left-1 bottom-2 top-2 w-0.5 rounded-r bg-ochre" />
      )}
      {label}
    </Link>
  );
}
