'use client';

import { useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import {
  ChevronDown,
  ChevronRight,
  FolderOpen,
  Loader2,
  MessageCircle,
  Plus,
  Search,
} from 'lucide-react';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';
import { usePermission } from '@/lib/hooks/usePermission';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { useProjectsQuery } from '@/hooks/useProjects';
import { useConversationDisplayTitle } from '@/hooks/useConversationDisplayTitle';
import { AppInput } from '@/components/design-system/AppInput';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { listConversations } from '@/lib/conversations';
import { useBusinessStore } from '@/lib/stores/business';
import { trackClick } from '@/lib/telemetry';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/design-system/AppAlertDialog';
import { ConversationItem, type Conversation } from '@/components/chat/ConversationItem';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { SkeletonInbox } from '@/components/states';

export default function ChatListPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const activeBusiness = useRef(activeBusinessId);
  activeBusiness.current = activeBusinessId;
  const tChat = useTranslations('chat');
  const tCommon = useTranslations('common');
  const tList = useTranslations('chat.list');
  const tSidebar = useTranslations('sidebar');
  const getDisplayTitle = useConversationDisplayTitle();
  const projectsQuery = useProjectsQuery();
  const [search, setSearch] = useState('');
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const createPermission = usePermission('content.create');
  const canCreate = createPermission.allowed;
  const canUpdate = usePermission('content.update').allowed;
  const canDelete = usePermission('content.delete').allowed;

  const {
    data: conversations = [],
    isPending: isLoading,
    isError,
    refetch,
  } = useQuery<Conversation[]>({
    queryKey: conversationsQueryKey(activeBusinessId),
    queryFn: () => listConversations(activeBusinessId!),
    enabled: !!activeBusinessId,
  });

  const { mutate: createConversation, isPending } = useMutation({
    mutationFn: (businessId: string) =>
      bizApi(businessId)
        .post<Conversation>(BIZ_API_PATHS.CONVERSATIONS.ROOT, { title: tChat('newConversation') })
        .then((r) => r.data),
    onSuccess: (conv: Conversation, businessId) => {
      trackClick('create_conversation', undefined, businessId);
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(businessId),
      });
      if (activeBusiness.current !== businessId) return;
      router.push(`/chat/${conv.id}`);
    },
    onError: () => toast.error(tCommon('connectionError')),
  });

  const {
    mutate: renameConversation,
    isPending: isRenaming,
    variables: renameVariables,
  } = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      bizApi(activeBusinessId!)
        .put<Conversation>(BIZ_API_PATHS.CONVERSATIONS.BY_ID(id), { title })
        .then((r) => r.data),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      }),
    onError: () => toast.error(tCommon('connectionError')),
  });

  const {
    mutate: regenerateTitle,
    isPending: isRegenerating,
    variables: regenerateId,
  } = useMutation({
    mutationFn: (id: string) =>
      bizApi(activeBusinessId!)
        .post(BIZ_API_PATHS.CONVERSATIONS.REGENERATE_TITLE(id))
        .then((r) => r.data),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(activeBusinessId),
      }),
    onError: (err: unknown) => {
      const axErr = err as AxiosError<{ message?: string }> | undefined;
      const msg = axErr?.response?.data?.message ?? tCommon('connectionError');
      toast.error(msg);
    },
  });

  const {
    mutate: deleteConversation,
    isPending: isDeleting,
    variables: deleteVariables,
  } = useMutation({
    mutationFn: ({ id, businessId }: { id: string; businessId: string }) =>
      bizApi(businessId).delete(BIZ_API_PATHS.CONVERSATIONS.BY_ID(id)),
    onSuccess: (_, { businessId }) => {
      trackClick('delete_conversation', undefined, businessId);
      queryClient.invalidateQueries({
        queryKey: conversationsQueryKey(businessId),
      });
      if (activeBusiness.current === businessId) setDeleteTarget(null);
    },
    onError: () => toast.error(tCommon('connectionError')),
  });

  const projectNames = new Map(
    (projectsQuery.data ?? []).map((project) => [project.id, project.name])
  );
  const query = search.trim().toLocaleLowerCase();
  const groups = new Map<string, { name: string; conversations: Conversation[] }>();
  for (const conv of conversations) {
    const id = conv.projectId || 'none';
    const name = conv.projectId
      ? (projectNames.get(conv.projectId) ?? tList('unavailableProject'))
      : tSidebar('unassigned');
    if (
      query &&
      ![getDisplayTitle(conv), conv.preview ?? '', name].some((value) =>
        value.toLocaleLowerCase().includes(query)
      )
    )
      continue;
    if (!groups.has(id)) groups.set(id, { name, conversations: [] });
    groups.get(id)!.conversations.push(conv);
  }
  const sortedGroups = [...groups].sort(([a, first], [b, second]) => {
    if (a === 'none') return -1;
    if (b === 'none') return 1;
    return first.name.localeCompare(second.name);
  });
  function handleCreate() {
    if (activeBusinessId && canCreate && !isPending) createConversation(activeBusinessId);
  }
  function renderCreateButton(empty = false) {
    return (
      <Button
        onClick={handleCreate}
        disabled={isPending || !activeBusinessId}
        aria-busy={isPending}
      >
        {isPending ? (
          <Loader2 aria-hidden size={16} className="mr-2 animate-spin motion-reduce:animate-none" />
        ) : (
          <Plus aria-hidden size={16} className="mr-2" />
        )}
        {isPending ? tList('creating') : empty ? tList('createFirst') : tChat('newConversation')}
      </Button>
    );
  }

  return (
    <div className="mx-auto max-w-[1040px] p-4 md:p-8">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-page-title">{tChat('heading')}</h1>
          <p className="mt-1 text-meta text-ink-soft">{tList('description')}</p>
        </div>
        {canCreate && renderCreateButton()}
      </div>
      {createPermission.isError && <ListLoadError onRetry={createPermission.refetch} />}
      {isLoading || projectsQuery.isPending ? (
        <SkeletonInbox rows={3} />
      ) : isError || projectsQuery.isError ? (
        <ListLoadError
          onRetry={() => {
            void refetch();
            void projectsQuery.refetch();
          }}
        />
      ) : conversations.length === 0 ? (
        <div
          role="status"
          className="flex flex-col items-center justify-center gap-4 rounded-lg border border-line bg-paper-raised px-4 py-12 text-center"
        >
          <MessageCircle aria-hidden size={40} className="text-ink-soft" />
          <div>
            <p className="text-document-title">{tChat('noConversations')}</p>
            <p className="mt-2 max-w-md text-meta text-ink-soft">
              {canCreate ? tList('emptyDescription') : tList('emptyReadOnly')}
            </p>
          </div>
          {canCreate && renderCreateButton(true)}
        </div>
      ) : (
        <div className="space-y-6">
          <div className="relative">
            <Search
              aria-hidden
              className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-ink-soft"
            />
            <AppInput
              type="search"
              aria-label={tList('search')}
              placeholder={tList('search')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="pl-9"
            />
          </div>
          {sortedGroups.length === 0 ? (
            <div
              role="status"
              className="rounded-lg border border-line bg-paper-raised p-8 text-center"
            >
              <p className="mb-4 text-ink-soft">{tList('noResults')}</p>
              <Button variant="outline" onClick={() => setSearch('')}>
                {tList('clearSearch')}
              </Button>
            </div>
          ) : (
            sortedGroups.map(([id, group]) => {
              const expanded = !!query || !collapsed.has(id);
              return (
                <section
                  key={id}
                  aria-label={group.name}
                  className="overflow-hidden rounded-lg border border-line bg-paper-raised"
                >
                  <h2>
                    <button
                      type="button"
                      aria-expanded={expanded}
                      aria-controls={`chat-group-${id}`}
                      onClick={() =>
                        setCollapsed((current) => {
                          const next = new Set(current);
                          if (next.has(id)) next.delete(id);
                          else next.add(id);
                          return next;
                        })
                      }
                      className="flex min-h-12 w-full items-center gap-2 bg-paper-sunken px-4 py-3 text-left"
                    >
                      {expanded ? (
                        <ChevronDown aria-hidden size={16} />
                      ) : (
                        <ChevronRight aria-hidden size={16} />
                      )}
                      <FolderOpen aria-hidden size={18} className="shrink-0 text-ink-soft" />
                      <span className="min-w-0 flex-1 break-words text-action">{group.name}</span>
                      <span className="shrink-0 text-meta text-ink-soft">
                        {tList('count', { count: group.conversations.length })}
                      </span>
                    </button>
                  </h2>
                  {expanded && (
                    <div id={`chat-group-${id}`}>
                      {group.conversations.map((conv) => (
                        <ConversationItem
                          key={conv.id}
                          conv={conv}
                          canUpdate={canUpdate}
                          canDelete={canDelete}
                          busy={
                            (isRenaming && renameVariables?.id === conv.id) ||
                            (isRegenerating && regenerateId === conv.id) ||
                            (isDeleting && deleteVariables?.id === conv.id)
                          }
                          onOpen={() => router.push(`/chat/${conv.id}`)}
                          onRename={(title) => renameConversation({ id: conv.id, title })}
                          onDelete={() => setDeleteTarget(conv.id)}
                          onRegenerateTitle={() => regenerateTitle(conv.id)}
                        />
                      ))}
                    </div>
                  )}
                </section>
              );
            })
          )}
        </div>
      )}

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && !isDeleting && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{tChat('deleteConversationTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {tChat('deleteConversationDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>{tCommon('cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="danger"
              disabled={isDeleting}
              aria-busy={isDeleting}
              onClick={(event) => {
                event.preventDefault();
                if (deleteTarget && activeBusinessId && !isDeleting)
                  deleteConversation({ id: deleteTarget, businessId: activeBusinessId });
              }}
            >
              {isDeleting && (
                <Loader2
                  aria-hidden
                  className="mr-2 h-4 w-4 animate-spin motion-reduce:animate-none"
                />
              )}
              {isDeleting ? tList('deleting') : tCommon('delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
