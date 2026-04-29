'use client';

import Link from 'next/link';
import { FolderOpen } from 'lucide-react';
import { cn } from '@/lib/utils';

// Phase 19 / Plan 19-02 / D-05 — size variants. The `xs` size is used by
// PinnedSection rows as a mini project-affiliation indicator (chats inside
// «Без проекта» get NO chip; only chats that belong to a real project are
// chipped on their pinned row to disambiguate the duplicated entry).
type Size = 'xs' | 'sm' | 'md';

const sizeClasses: Record<Size, string> = {
  xs: 'px-1 py-0 text-[10px] gap-1',
  sm: 'px-2 py-0.5 text-xs gap-1.5', // current default
  md: 'px-3 py-1 text-sm gap-2',
};

const iconSize: Record<Size, number> = {
  xs: 10,
  sm: 12,
  md: 14,
};

interface Props {
  projectId: string | null;
  projectName?: string;
  size?: Size;
}

const UNASSIGNED_LABEL = 'Без проекта';

const chipBase = 'inline-flex items-center rounded-md border transition-colors';

export function ProjectChip({ projectId, projectName, size = 'sm' }: Props) {
  if (projectId == null) {
    return (
      <span
        aria-label="Чат не привязан к проекту"
        className={cn(
          chipBase,
          sizeClasses[size],
          'cursor-default border-border italic text-muted-foreground'
        )}
      >
        <FolderOpen size={iconSize[size]} />
        <span className="truncate">{UNASSIGNED_LABEL}</span>
      </span>
    );
  }

  return (
    <Link
      href={`/projects/${projectId}`}
      aria-label={`Открыть проект «${projectName ?? ''}»`}
      className={cn(
        chipBase,
        sizeClasses[size],
        'hover:border-primary/40 border-border text-muted-foreground hover:text-foreground'
      )}
    >
      <FolderOpen size={iconSize[size]} />
      <span className="truncate">{projectName ?? ''}</span>
    </Link>
  );
}
