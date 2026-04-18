'use client';

import { toast } from 'sonner';
import {
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuItem,
  DropdownMenuPortal,
} from '@/components/ui/dropdown-menu';
import { useProjectsQuery } from '@/hooks/useProjects';
import { useMoveConversation } from '@/hooks/useConversations';
import { cn } from '@/lib/utils';
import type { Project } from '@/types/project';

interface Props {
  conversationId: string;
  currentProjectId: string | null;
}

const UNASSIGNED_LABEL = 'Без проекта';

export function MoveChatMenuItem({ conversationId, currentProjectId }: Props) {
  const { data: projects } = useProjectsQuery();
  const move = useMoveConversation();

  const sortedProjects: Project[] = [...(projects ?? [])].sort((a, b) =>
    a.name.localeCompare(b.name, 'ru')
  );

  function handleMove(destId: string | null, destName: string) {
    move.mutate(
      { id: conversationId, projectId: destId, previousProjectId: currentProjectId },
      {
        onSuccess: () => {
          toast.success(`Чат перемещён в «${destName}»`, {
            duration: 5000,
            action: {
              label: 'Отменить',
              onClick: () => {
                move.mutate({
                  id: conversationId,
                  projectId: currentProjectId,
                  previousProjectId: destId,
                });
              },
            },
          });
        },
        onError: (err) => {
          const message =
            err instanceof Error && 'response' in err
              ? ((err as { response?: { data?: { error?: string } } }).response?.data?.error ?? '')
              : err instanceof Error
                ? err.message
                : '';
          toast.error('Не удалось переместить чат', { description: message });
        },
      }
    );
  }

  // "Без проекта" disabled when the chat is already unassigned; other projects
  // disabled when they equal currentProjectId.
  const unassignedDisabled = currentProjectId == null;
  const otherProjects = sortedProjects;
  const hasOtherDestinations =
    !unassignedDisabled || otherProjects.some((p) => p.id !== currentProjectId);

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>Переместить в…</DropdownMenuSubTrigger>
      <DropdownMenuPortal>
        <DropdownMenuSubContent>
          {!hasOtherDestinations ? (
            <DropdownMenuItem disabled className="italic text-muted-foreground">
              Других проектов пока нет.
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem
                disabled={unassignedDisabled}
                onSelect={(e) => {
                  e.preventDefault();
                  handleMove(null, UNASSIGNED_LABEL);
                }}
                className={cn('italic text-muted-foreground')}
              >
                {UNASSIGNED_LABEL}
              </DropdownMenuItem>
              {otherProjects.map((p) => (
                <DropdownMenuItem
                  key={p.id}
                  disabled={p.id === currentProjectId}
                  onSelect={(e) => {
                    e.preventDefault();
                    handleMove(p.id, p.name);
                  }}
                >
                  {p.name}
                </DropdownMenuItem>
              ))}
            </>
          )}
        </DropdownMenuSubContent>
      </DropdownMenuPortal>
    </DropdownMenuSub>
  );
}
