'use client';

import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

const KNOWN_SYSTEM_ROLES = ['owner', 'admin', 'editor', 'viewer'] as const;
type SystemRole = (typeof KNOWN_SYSTEM_ROLES)[number];

/**
 * Cap custom role-name display so long names (e.g. «marketing-strategist-eu»)
 * don't blow out the popover row width. The full string is preserved in the
 * `title=` attribute so hover + assistive tech still surface it. 24 was chosen
 * to fit a 200px row at `text-[11px]` mono with room for the avatar gap.
 */
const ROLE_LABEL_MAX_CHARS = 24;
const ROLE_LABEL_TRUNCATE_AT = ROLE_LABEL_MAX_CHARS - 1; // room for the ellipsis glyph

function isSystemRole(name: string): name is SystemRole {
  return (KNOWN_SYSTEM_ROLES as readonly string[]).includes(name);
}

export interface RolePillProps {
  /** Role name as returned by the backend (`role.name`). */
  roleName: string;
  /** Optional size: default sits inside a switcher row; `lg` is used on the accept page. */
  size?: 'sm' | 'lg';
  className?: string;
}

/**
 * Decorative `<span>` rendering the role label in lowercase mono kicker style.
 *
 * Visual contract per UI-SPEC §S-1 / Color → "Role pill (mono kicker variant)":
 *   `bg-paper-sunken text-ink-soft font-mono text-[11px] tracking-[0.04em]`
 *   uppercase OVERRIDDEN to lowercase (the kicker primitive forces uppercase,
 *   so we re-implement the visual tokens here instead of importing it).
 *
 * Labels for the four system roles are pulled from `team.roles.*`; custom
 * roles fall back to the lowercased `role.name`, truncated to 24 chars with
 * the full string preserved in the `title=` attribute for hover/SR readout.
 */
export function RolePill({ roleName, size = 'sm', className }: RolePillProps) {
  const tRoles = useTranslations('team.roles');

  const label = isSystemRole(roleName) ? tRoles(roleName) : roleName.toLowerCase();
  const truncated = label.length > ROLE_LABEL_MAX_CHARS;
  const display = truncated ? `${label.slice(0, ROLE_LABEL_TRUNCATE_AT)}…` : label;
  const title = truncated ? label : undefined;

  return (
    <span
      title={title}
      className={cn(
        'inline-flex items-center self-start rounded-md bg-paper-sunken font-mono text-[11px] tracking-[0.04em] text-ink-soft',
        size === 'lg' ? 'px-3 py-1 text-xs' : 'px-2 py-1',
        className
      )}
    >
      {display}
    </span>
  );
}
