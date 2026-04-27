'use client';

import { useRef, useEffect, useState, useMemo } from 'react';
import { Send } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ChatHeader } from './ChatHeader';
import { MessageBubble } from './MessageBubble';
import { ProjectChip } from './ProjectChip';
import { ProjectPickerChip } from './ProjectPickerChip';
import { ToolApprovalCard } from './ToolApprovalCard';
import { ExpiredApprovalBanner } from './ExpiredApprovalBanner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useChat } from '@/hooks/useChat';
import { useProjectsQuery } from '@/hooks/useProjects';
import { useMoveConversation, conversationsQueryKey } from '@/hooks/useConversations';
import { DEFAULT_QUICK_ACTIONS } from '@/lib/quick-actions';
import { api } from '@/lib/api';
import type { Conversation } from '@/lib/conversations';

async function fetchConversation(id: string): Promise<Conversation> {
  const { data } = await api.get<Conversation>(`/conversations/${id}`);
  return data;
}

export function ChatWindow({ conversationId }: { conversationId: string }) {
  const { messages, isLoading, isStreaming, pendingApproval, resolveApproval, sendMessage } =
    useChat(conversationId);
  const [input, setInput] = useState('');
  const bottomRef = useRef<HTMLDivElement>(null);
  const qc = useQueryClient();

  // Invariant 9: the composer is disabled whenever a batch is awaiting
  // the user's decision OR while a message is streaming. Both conditions
  // must flow through a single flag so the Input and Send Button stay
  // in sync.
  const composerDisabled = isStreaming || pendingApproval !== null;

  const { data: conversation } = useQuery<Conversation>({
    queryKey: ['conversations', conversationId],
    queryFn: () => fetchConversation(conversationId),
    enabled: !!conversationId,
  });

  const { data: projects } = useProjectsQuery();
  const move = useMoveConversation();

  const currentProject = useMemo(() => {
    if (!conversation?.projectId || !projects) return null;
    return projects.find((p) => p.id === conversation.projectId) ?? null;
  }, [conversation?.projectId, projects]);

  const quickActions =
    currentProject?.quickActions && currentProject.quickActions.length > 0
      ? currentProject.quickActions
      : DEFAULT_QUICK_ACTIONS;

  const showEmptyState = messages.length === 0 && !isLoading;

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || composerDisabled) return;
    setInput('');
    await sendMessage(text);
  };

  const handlePickerChange = (projectId: string | null) => {
    if (!conversation) return;
    if ((conversation.projectId ?? null) === projectId) return;
    move.mutate(
      {
        id: conversationId,
        projectId,
        previousProjectId: conversation.projectId ?? null,
      },
      {
        onSuccess: () => {
          void qc.invalidateQueries({ queryKey: ['conversations', conversationId] });
          void qc.invalidateQueries({ queryKey: conversationsQueryKey });
        },
        onError: () => {
          toast.error('Не удалось переместить чат');
        },
      }
    );
  };

  return (
    <div className="flex h-full flex-col">
      {/* Chat header — Phase 18 / D-11 (USER OVERRIDE) Landmine 1:
          isolated, memoized subtree subscribed via useQuery `select` to a
          primitive string. Rendered as a SIBLING of the message list and
          composer below so title changes do not destroy composer focus or
          scroll position. */}
      {!showEmptyState && (
        <ChatHeader
          conversationId={conversationId}
          rightSlot={
            <ProjectChip
              projectId={currentProject?.id ?? null}
              projectName={currentProject?.name}
            />
          }
        />
      )}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-6">
        {isLoading ? (
          <div className="flex h-full items-center justify-center">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
          </div>
        ) : messages.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-4">
            <ProjectPickerChip
              value={conversation?.projectId ?? null}
              onChange={handlePickerChange}
            />
            <p className="text-lg text-gray-400">Чем могу помочь?</p>
            <div className="flex flex-wrap justify-center gap-2">
              {quickActions.map((action) => (
                <button
                  key={action}
                  type="button"
                  onClick={() => sendMessage(action)}
                  disabled={composerDisabled}
                  className="rounded-full border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {action}
                </button>
              ))}
            </div>
          </div>
        ) : (
          messages.map((msg) => <MessageBubble key={msg.id} message={msg} />)
        )}
        <div ref={bottomRef} />
      </div>

      {/* Expired approval banner — sits above the card; owned by Plan 17-05. */}
      {pendingApproval?.status === 'expired' && <ExpiredApprovalBanner />}

      {/* Inline approval card — renders only when a pending batch exists. */}
      {pendingApproval?.status === 'pending' && (
        <div className="border-t bg-background px-4 py-4">
          <ToolApprovalCard batch={pendingApproval} onSubmit={resolveApproval} />
        </div>
      )}

      {/* Input */}
      <div className="flex gap-2 border-t bg-white p-4">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && void handleSend()}
          placeholder="Напишите сообщение..."
          disabled={composerDisabled}
          className="flex-1"
        />
        <Button onClick={handleSend} disabled={composerDisabled || !input.trim()}>
          <Send size={16} />
        </Button>
      </div>
    </div>
  );
}
