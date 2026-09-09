'use client';

import { Loader2 } from 'lucide-react';
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
import { extractApiErrorCode } from '@/lib/resolveErrorMap';
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
    if (move.isPending) return;
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
              ? (extractApiErrorCode(err) ?? '')
              : err instanceof Error
                ? err.message
                : '';
          toast.error(tMove('moveError'), { description: message });
        },
      }
    );
  }

  const unassignedDisabled = currentProjectId == null;
  const otherProjects = sortedProjects;
  const hasOtherDestinations =
    !unassignedDisabled || otherProjects.some((p) => p.id !== currentProjectId);

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger aria-busy={move.isPending}>
        {move.isPending && (
          <Loader2 aria-hidden className="mr-2 h-4 w-4 animate-spin motion-reduce:animate-none" />
        )}
        {move.isPending ? tMove('moving') : tMove('trigger')}
      </DropdownMenuSubTrigger>
      <DropdownMenuPortal>
        <DropdownMenuSubContent>
          {!hasOtherDestinations ? (
            <DropdownMenuItem disabled className="italic text-muted-foreground">
              {tMove('noProjects')}
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem
                disabled={unassignedDisabled || move.isPending}
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
                  disabled={p.id === currentProjectId || move.isPending}
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
