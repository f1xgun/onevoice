'use client';

import { useTranslations } from 'next-intl';
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

export function MoveChatMenuItem({ conversationId, currentProjectId }: Props) {
  const tMove = useTranslations('chat.moveMenu');
  const { data: projects } = useProjectsQuery();
  const move = useMoveConversation();

  const unassignedLabel = tMove('unassignedLabel');

  const sortedProjects: Project[] = [...(projects ?? [])].sort((a, b) =>
    a.name.localeCompare(b.name, 'ru')
  );

  function handleMove(destId: string | null, destName: string) {
    move.mutate(
      { id: conversationId, projectId: destId, previousProjectId: currentProjectId },
      {
        onSuccess: () => {
          toast.success(tMove('movedTo', { name: destName }), {
            duration: 5000,
            action: {
              label: tMove('undo'),
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
          toast.error(tMove('moveError'), { description: message });
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
      <DropdownMenuSubTrigger>{tMove('trigger')}</DropdownMenuSubTrigger>
      <DropdownMenuPortal>
        <DropdownMenuSubContent>
          {!hasOtherDestinations ? (
            <DropdownMenuItem disabled className="italic text-muted-foreground">
              {tMove('noProjects')}
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem
                disabled={unassignedDisabled}
                onSelect={(e) => {
                  e.preventDefault();
                  handleMove(null, unassignedLabel);
                }}
                className={cn('italic text-muted-foreground')}
              >
                {unassignedLabel}
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
