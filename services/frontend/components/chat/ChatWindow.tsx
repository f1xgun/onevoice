'use client';

import { useRef, useEffect, useState, useMemo } from 'react';
import { Send } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { MessageBubble } from './MessageBubble';
import { ProjectChip } from './ProjectChip';
import { ProjectPickerChip } from './ProjectPickerChip';
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
  const { messages, isLoading, isStreaming, sendMessage } = useChat(conversationId);
  const [input, setInput] = useState('');
  const bottomRef = useRef<HTMLDivElement>(null);
  const qc = useQueryClient();

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
    if (!text || isStreaming) return;
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
      {/* Chat header */}
      {!showEmptyState && (
        <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b bg-background px-4">
          <span className="truncate text-sm font-medium">
            {conversation?.title ?? ''}
          </span>
          <ProjectChip
            projectId={currentProject?.id ?? null}
            projectName={currentProject?.name}
          />
        </div>
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
                  disabled={isStreaming}
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

      {/* Input */}
      <div className="flex gap-2 border-t bg-white p-4">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && void handleSend()}
          placeholder="Напишите сообщение..."
          disabled={isStreaming}
          className="flex-1"
        />
        <Button onClick={handleSend} disabled={isStreaming || !input.trim()}>
          <Send size={16} />
        </Button>
      </div>
    </div>
  );
}
