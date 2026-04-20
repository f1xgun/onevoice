import { useState, useCallback, useRef, useEffect } from 'react';
import { useAuthStore } from '@/lib/auth';
import { trackEvent } from '@/lib/telemetry';
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
      id: (event.tool_call_id as string) || crypto.randomUUID(),
      name: event.tool_name as string,
      args: (event.tool_args as Record<string, unknown>) ?? {},
      status: 'pending',
    };
    return { ...msg, toolCalls: [...(msg.toolCalls ?? []), toolCall] };
  }

  if (type === 'tool_result') {
    // Correlate by orchestrator-issued tool_call_id — duplicate tool names
    // in a single batch (e.g., two send_channel_post calls) would collapse
    // under a name-based match.
    const callID = event.tool_call_id as string | undefined;
    const toolName = event.tool_name as string;
    const calls = msg.toolCalls ?? [];
    let matchIdx = callID ? calls.findIndex((t) => t.id === callID) : -1;
    if (matchIdx === -1) {
      // Fallback: oldest pending with that name.
      matchIdx = calls.findIndex((t) => t.name === toolName && t.status === 'pending');
    }
    if (matchIdx === -1) return msg;
    const updated = calls.map((tc, i) =>
      i === matchIdx
        ? {
            ...tc,
            result: event.result as Record<string, unknown>,
            error: event.error as string | undefined,
            status: (event.error ? 'error' : 'done') as ToolCall['status'],
          }
        : tc
    );
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
        if (!r.ok) return null;
        return r.json() as Promise<
          ApiMessage[] | { messages: ApiMessage[]; pendingApprovals?: unknown[] }
        >;
      })
      .then((payload) => {
        // Phase 16 changed the shape from `ApiMessage[]` to
        // `{messages, pendingApprovals}`. Accept either so older responses
        // (test fixtures, local dev runs without Phase-16 MongoDB) still work.
        const apiMsgs: ApiMessage[] | null = Array.isArray(payload)
          ? payload
          : payload && Array.isArray((payload as { messages?: ApiMessage[] }).messages)
            ? (payload as { messages: ApiMessage[] }).messages
            : null;
        if (apiMsgs) {
          setMessages(
            apiMsgs.map((m) => {
              const toolCalls: ToolCall[] | undefined =
                m.toolCalls && m.toolCalls.length > 0
                  ? m.toolCalls.map((tc) => {
                      const result = m.toolResults?.find((r) => r.toolCallId === tc.id);
                      // No matching tool_result → the run was interrupted
                      // before this tool produced one. Surface it as
                      // 'aborted' so the UI doesn't mislead with a green
                      // checkmark.
                      const status: ToolCall['status'] = result
                        ? result.isError
                          ? 'error'
                          : 'done'
                        : 'aborted';
                      return {
                        id: tc.id,
                        name: tc.name,
                        args: tc.arguments ?? {},
                        result: result && !result.isError ? result.content : undefined,
                        error: result?.isError
                          ? ((result.content?.error as string) ?? 'error')
                          : undefined,
                        status,
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

      trackEvent('chat_send', 'send_message', {
        metadata: { conversationId: conversationId ?? '' },
      });

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
