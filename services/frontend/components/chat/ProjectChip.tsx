'use client';

import Link from 'next/link';
import { FolderOpen } from 'lucide-react';
import { cn } from '@/lib/utils';

interface Props {
  projectId: string | null;
  projectName?: string;
}

const UNASSIGNED_LABEL = 'Без проекта';

const chipBase =
  'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs transition-colors';

export function ProjectChip({ projectId, projectName }: Props) {
  if (projectId == null) {
    return (
      <span
        aria-label="Чат не привязан к проекту"
        className={cn(chipBase, 'cursor-default border-border italic text-muted-foreground')}
      >
        <FolderOpen size={12} />
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
        'border-border text-muted-foreground hover:border-primary/40 hover:text-foreground'
      )}
    >
      <FolderOpen size={12} />
      <span className="truncate">{projectName ?? ''}</span>
    </Link>
  );
}
