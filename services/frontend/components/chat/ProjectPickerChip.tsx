'use client';

import { ChevronDown } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useProjectsQuery } from '@/hooks/useProjects';
import { cn } from '@/lib/utils';

interface Props {
  value: string | null;
  onChange: (projectId: string | null) => void;
}

const UNASSIGNED_LABEL = 'Без проекта';

export function ProjectPickerChip({ value, onChange }: Props) {
  const { data: projects } = useProjectsQuery();
  const sorted = [...(projects ?? [])].sort((a, b) => a.name.localeCompare(b.name, 'ru'));

  const currentName =
    value == null
      ? UNASSIGNED_LABEL
      : (sorted.find((p) => p.id === value)?.name ?? UNASSIGNED_LABEL);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="text-muted-foreground hover:text-foreground"
        >
          <span className="truncate">Проект: {currentName}</span>
          <ChevronDown size={12} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
          Куда сохранить чат?
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault();
            onChange(null);
          }}
          className={cn('italic text-muted-foreground')}
        >
          {UNASSIGNED_LABEL}
        </DropdownMenuItem>
        {sorted.map((p) => (
          <DropdownMenuItem
            key={p.id}
            onSelect={(e) => {
              e.preventDefault();
              onChange(p.id);
            }}
          >
            {p.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
