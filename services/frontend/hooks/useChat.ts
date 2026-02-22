import { useState, useCallback, useRef, useEffect } from 'react';
import { useAuthStore } from '@/lib/auth';
import type { Message, ToolCall } from '@/types/chat';

// Exported for unit testing
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null;
  try {
    return JSON.parse(line.slice(6));
  } catch {
    return null;
  }
}

export function applySSEEvent(msg: Message, event: Record<string, unknown>): Message {
  const type = event.type as string;

  if (type === 'text') {
    return { ...msg, content: msg.content + (event.content as string) };
  }

  if (type === 'tool_call') {
    const toolCall: ToolCall = {
      id: crypto.randomUUID(),
      name: event.tool_name as string,
      args: (event.tool_args as Record<string, unknown>) ?? {},
      status: 'pending',
    };
    return { ...msg, toolCalls: [...(msg.toolCalls ?? []), toolCall] };
  }

  if (type === 'tool_result') {
    const toolName = event.tool_name as string;
    const updated = (msg.toolCalls ?? []).map((tc, i) => {
      // Find the first pending tool with this name
      const firstPendingIdx = (msg.toolCalls ?? []).findIndex(
        (t) => t.name === toolName && t.status === 'pending'
      );
      return i === firstPendingIdx
        ? {
            ...tc,
            result: event.result as Record<string, unknown>,
            error: event.error as string | undefined,
            status: (event.error ? 'error' : 'done') as ToolCall['status'],
          }
        : tc;
    });
    return { ...msg, toolCalls: updated };
  }

  if (type === 'done') {
    return { ...msg, status: 'done' };
  }

  return msg;
}

interface ApiToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
}

interface ApiToolResult {
  toolCallId: string;
  content: Record<string, unknown>;
  isError: boolean;
}

interface ApiMessage {
  id: string;
  role: string;
  content: string;
  toolCalls?: ApiToolCall[];
  toolResults?: ApiToolResult[];
}

export function useChat(conversationId: string) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const isStreamingRef = useRef(false);
  const accessToken = useAuthStore((s) => s.accessToken);
  const abortRef = useRef<AbortController | null>(null);

  // Load existing messages on mount
  useEffect(() => {
    setIsLoading(true);
    fetch(`/api/v1/conversations/${conversationId}/messages`, {
      headers: { Authorization: `Bearer ${accessToken}` },
    })
      .then((r) => {
        if (!r.ok) return [];
        return r.json() as Promise<ApiMessage[]>;
      })
      .then((apiMsgs) => {
        if (Array.isArray(apiMsgs)) {
          setMessages(
            apiMsgs.map((m) => {
              const toolCalls: ToolCall[] | undefined =
                m.toolCalls && m.toolCalls.length > 0
                  ? m.toolCalls.map((tc) => {
                      const result = m.toolResults?.find((r) => r.toolCallId === tc.id);
                      return {
                        id: tc.id,
                        name: tc.name,
                        args: tc.arguments ?? {},
                        result: result && !result.isError ? result.content : undefined,
                        error: result?.isError
                          ? (result.content?.error as string) ?? 'error'
                          : undefined,
                        status: result ? (result.isError ? ('error' as const) : ('done' as const)) : ('done' as const),
                      };
                    })
                  : undefined;
              return {
                id: m.id,
                role: m.role as 'user' | 'assistant',
                content: m.content,
                toolCalls,
                status: 'done' as const,
              };
            })
          );
        }
      })
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, [conversationId, accessToken]);

  const sendMessage = useCallback(
    async (text: string) => {
      if (isStreamingRef.current) return;

      const userMessage: Message = {
        id: crypto.randomUUID(),
        role: 'user',
        content: text,
        status: 'done',
      };

      const assistantMessage: Message = {
        id: crypto.randomUUID(),
        role: 'assistant',
        content: '',
        toolCalls: [],
        status: 'streaming',
      };

      setMessages((prev) => [...prev, userMessage, assistantMessage]);
      setIsStreaming(true);
      isStreamingRef.current = true;

      const controller = new AbortController();
      abortRef.current = controller;

      try {
        const response = await fetch(`/api/v1/chat/${conversationId}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${accessToken}`,
          },
          body: JSON.stringify({ message: text }),
          signal: controller.signal,
        });

        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }

        const reader = response.body!.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() ?? '';

          for (const line of lines) {
            const event = parseSSELine(line.trim());
            if (!event) continue;
            setMessages((prev) => {
              const last = prev[prev.length - 1];
              if (last.role !== 'assistant') return prev;
              return [...prev.slice(0, -1), applySSEEvent(last, event)];
            });
          }
        }

        // Flush any remaining content in buffer
        if (buffer.trim()) {
          const event = parseSSELine(buffer.trim());
          if (event) {
            setMessages((prev) => {
              const last = prev[prev.length - 1];
              if (last.role !== 'assistant') return prev;
              return [...prev.slice(0, -1), applySSEEvent(last, event)];
            });
          }
        }
      } catch (error: unknown) {
        if ((error as Error).name === 'AbortError') return;
        setMessages((prev) => {
          const last = prev[prev.length - 1];
          if (last.role !== 'assistant') return prev;
          return [...prev.slice(0, -1), { ...last, content: 'Ошибка соединения', status: 'done' }];
        });
      } finally {
        setIsStreaming(false);
        isStreamingRef.current = false;
      }
    },
    [conversationId, accessToken]
  );

  const stop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  return { messages, isLoading, isStreaming, sendMessage, stop };
}
