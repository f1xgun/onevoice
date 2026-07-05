'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { cn } from '@/lib/utils';
import { usePermission } from '@/lib/hooks/usePermission';

export interface SettingsNavLinkProps {
  href: string;
  label: string;
  /**
   * Optional permission gate. When set, the link renders only for actors
   * whose active role holds `perm` (matches the page-level RequirePermission
   * gate). Omit it for tabs that everyone may see.
   */
  perm?: string;
}

export function SettingsNavLink({ href, label, perm }: SettingsNavLinkProps) {
  const pathname = usePathname();
  const { allowed } = usePermission(perm ?? '');
  const isActive = pathname === href;

  if (perm && !allowed) return null;
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
