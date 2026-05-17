'use client';

import { useMemo } from 'react';
import { useParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { Bookmark, Plus, Settings } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { EmptyFrame } from '@/components/states';
import { useConversationsQuery, useCreateConversation } from '@/hooks/useConversations';
import { useProjectQuery } from '@/hooks/useProjects';
import { usePermission } from '@/lib/hooks/usePermission';
import { cn } from '@/lib/utils';
import type { Conversation } from '@/lib/conversations';

export default function ProjectChatsPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const id = params?.id ?? '';

  const tProjects = useTranslations('projects');
  const tChat = useTranslations('chat');

  const { data: project, isLoading: projectLoading } = useProjectQuery(id);
  const { data: conversations, isLoading: conversationsLoading } = useConversationsQuery();
  const createConversation = useCreateConversation();
  const canCreate = usePermission('content.create').allowed;

  // Project-scoped slice. Pinned first, then most-recent-activity desc.
  // `lastMessageAt` is nullable on freshly-created chats (no messages yet);
  // fall back to updatedAt → createdAt so the order stays stable.
  const chats = useMemo<Conversation[]>(() => {
    const list = (conversations ?? []).filter((c) => c.projectId === id);
    const recencyKey = (c: Conversation) => c.lastMessageAt ?? c.updatedAt ?? c.createdAt;
    return [...list].sort((a, b) => {
      const aPinned = a.pinnedAt != null;
      const bPinned = b.pinnedAt != null;
      if (aPinned !== bPinned) return aPinned ? -1 : 1;
      return recencyKey(b).localeCompare(recencyKey(a));
    });
  }, [conversations, id]);

  async function handleCreate() {
    try {
      const conv = await createConversation.mutateAsync({
        title: tChat('newConversation'),
        projectId: id,
      });
      router.push(`/chat/${conv.id}`);
    } catch {
      toast.error(tProjects('errorCreateChat'));
    }
  }

  const newChatButton = canCreate ? (
    <Button
      variant="primary"
      size="md"
      onClick={() => void handleCreate()}
      disabled={createConversation.isPending}
    >
      <Plus size={16} aria-hidden />
      {tProjects('newChat')}
    </Button>
  ) : null;

  const headerActions = (
    <div className="flex items-center gap-2">
      <Button asChild variant="ghost" size="md">
        <Link href={`/projects/${id}`} aria-label={tProjects('settings')}>
          <Settings size={16} aria-hidden />
          {tProjects('settings')}
        </Link>
      </Button>
      {newChatButton}
    </div>
  );

  if (projectLoading || conversationsLoading) {
    return (
      <>
        <PageHeader title={tProjects('fallbackTitle')} />
        <div className="mx-auto w-full max-w-2xl space-y-3 px-4 pb-10 sm:px-12 sm:pb-16">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      </>
    );
  }

  if (!project) {
    return (
      <>
        <PageHeader title={tProjects('fallbackTitle')} />
        <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
          <div className="border-[var(--ov-danger)]/40 rounded-lg border bg-[var(--ov-danger-soft)] p-6 text-sm text-[var(--ov-danger)]">
            {tProjects('errorLoad')}
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      <PageHeader title={project.name} sub={project.description} actions={headerActions} />

      <div className="mx-auto w-full max-w-2xl px-4 pb-10 sm:px-12 sm:pb-16">
        {chats.length === 0 ? (
          <EmptyFrame
            title={tProjects('chats.empty.title')}
            body={tProjects('chats.empty.body')}
            action={newChatButton}
          />
        ) : (
          <section className="overflow-hidden rounded-lg border border-line bg-paper-raised">
            <div className="flex items-center justify-between border-b border-line-soft px-5 py-3">
              <MonoLabel>{tProjects('chats.sectionLabel')}</MonoLabel>
              <MonoLabel>{chats.length}</MonoLabel>
            </div>
            <ul className="divide-y divide-line-soft">
              {chats.map((chat) => (
                <li key={chat.id}>
                  <ChatRow chat={chat} fallbackTitle={tChat('newConversation')} />
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </>
  );
}

function ChatRow({ chat, fallbackTitle }: { chat: Conversation; fallbackTitle: string }) {
  const title = chat.title?.trim() || fallbackTitle;
  const ts = chat.lastMessageAt ?? chat.updatedAt ?? chat.createdAt;
  const when = format(new Date(ts), 'd MMM · HH:mm', { locale: ru });
  const pinned = chat.pinnedAt != null;

  return (
    <Link
      href={`/chat/${chat.id}`}
      className={cn(
        'flex items-center gap-3 px-5 py-3.5 transition-colors',
        'hover:bg-paper-sunken focus-visible:bg-paper-sunken focus-visible:outline-none'
      )}
    >
      {pinned && <Bookmark size={14} className="shrink-0 text-yellow-400" aria-hidden />}
      <span className="min-w-0 flex-1 truncate text-sm text-ink">{title}</span>
      <MonoLabel tone="mid" className="shrink-0 normal-case tracking-normal">
        {when}
      </MonoLabel>
    </Link>
  );
}
