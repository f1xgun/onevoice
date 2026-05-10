// useChat — owns Message[] + isLoading + isStreaming + sendMessage + stop
// (Phase 19, plan 19-10, D-19). The pendingApproval slice lives in the
// sibling `usePendingApprovalFlow`; SSE `tool_approval_required` and GET
// /messages hydration both flow out through the `onApprovalRequired`
// callback. Resume frames flow back via the public `appendSSEEvent`.
//
// RBAC (plan 02-09): all fetch URLs are business-scoped via the active
// business id from `useBusinessStore`. The conversation list invalidation
// uses `conversationsQueryKey(activeBusinessId)` so per-business cache
// partitioning stays intact across SSE 'done' events.

import { useState, useCallback, useRef, useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { API_BASE_URL } from '@/lib/constants/apiPaths';
import { getTranslator } from '@/lib/i18n/translator';
import { applySSEEvent, consumeSSEStream } from '@/lib/sse';
import { trackEvent } from '@/lib/telemetry';
import type { Message, PendingApproval, PendingApprovalCall, ToolCall } from '@/types/chat';

// Typed cast + defensive defaults. Preserves status === 'expired' so the
// UI layer owns the render decision (`ExpiredApprovalBanner`). Lives here
// because useChat is the sole fetcher of GET /messages.
function normalizePendingApproval(raw: unknown): PendingApproval | null {
  if (!raw || typeof raw !== 'object') return null;
  const r = raw as Record<string, unknown>;
  const callsRaw = Array.isArray(r.calls) ? (r.calls as unknown[]) : [];
  const calls: PendingApprovalCall[] = callsRaw.map((c) => {
    const cr = (c ?? {}) as Record<string, unknown>;
    return {
      callId: (cr.callId as string) ?? '',
      toolName: (cr.toolName as string) ?? '',
      args: (cr.args as Record<string, unknown>) ?? {},
      editableFields: Array.isArray(cr.editableFields) ? (cr.editableFields as string[]) : [],
      floor: (cr.floor as string) ?? 'manual',
    };
  });
  const status: PendingApproval['status'] = r.status === 'expired' ? 'expired' : 'pending';
  return {
    batchId: (r.batchId as string) ?? '',
    conversationId: r.conversationId as string | undefined,
    status,
    calls,
    expiresAt: r.expiresAt as string | undefined,
    createdAt: (r.createdAt as string) ?? new Date().toISOString(),
  };
}

// Module-level translator for the SSE-stream failure branch below. Strings
// are static, so a once-per-module lookup matches the rest of `lib/`.
// (D-AM-01: `usePendingApprovalFlow` keeps its own copy.)
const tCommon = getTranslator('common');

// Business-scoped URL builders. Kept inline here (rather than centralised
// in API_STREAM_PATHS) because every call site needs to forward the
// nullable activeBusinessId from the store and gracefully fall back to the
// legacy non-scoped path when no business is active — moving the fallback
// into the constants module would require duplicating both shapes there.
function messagesUrl(activeBusinessId: string | null, conversationId: string): string {
  return activeBusinessId
    ? `${API_BASE_URL}/businesses/${activeBusinessId}/conversations/${conversationId}/messages`
    : `${API_BASE_URL}/conversations/${conversationId}/messages`;
}

function chatUrl(activeBusinessId: string | null, conversationId: string): string {
  return activeBusinessId
    ? `${API_BASE_URL}/businesses/${activeBusinessId}/chat/${conversationId}`
    : `${API_BASE_URL}/chat/${conversationId}`;
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

interface UseChatOptions {
  conversationId: string;
  // Wired by the parent component to `usePendingApprovalFlow.setPending`.
  // Fired when a chat SSE stream emits `tool_approval_required`.
  onApprovalRequired?: (approval: PendingApproval) => void;
}

export function useChat({ conversationId, onApprovalRequired }: UseChatOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const isStreamingRef = useRef(false);
  const accessToken = useAuthStore((s) => s.accessToken);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const abortRef = useRef<AbortController | null>(null);
  // SSE 'done' invalidates conversationsQueryKey(activeBusinessId) for
  // out-of-band auto-title pickup. NEVER mux titles into chat SSE.
  const queryClient = useQueryClient();

  // Stable ref for the parent's onApprovalRequired so the SSE-handler
  // closure stays cheap to recreate.
  const onApprovalRequiredRef = useRef<((approval: PendingApproval) => void) | undefined>(
    onApprovalRequired
  );
  useEffect(() => {
    onApprovalRequiredRef.current = onApprovalRequired;
  });

  // Mount-load: legacy ApiMessage[] or {messages, pendingApprovals} envelope.
  // Sole /messages round trip; envelope's first batch fires onApprovalRequired.
  useEffect(() => {
    setIsLoading(true);
    fetch(messagesUrl(activeBusinessId, conversationId), {
      headers: { Authorization: `Bearer ${accessToken}` },
    })
      .then((r) => {
        if (!r.ok) return null;
        return r.json() as Promise<
          ApiMessage[] | { messages: ApiMessage[]; pendingApprovals?: unknown[] }
        >;
      })
      .then((payload) => {
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
                      // No tool_result → run was interrupted; mark 'aborted'
                      // so the UI doesn't show a green checkmark.
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
        // Hydration: surface the first persisted batch to the sibling hook.
        if (payload && !Array.isArray(payload)) {
          const pendings = (payload as { pendingApprovals?: unknown[] }).pendingApprovals;
          if (Array.isArray(pendings) && pendings.length > 0) {
            const normalized = normalizePendingApproval(pendings[0]);
            if (normalized) onApprovalRequiredRef.current?.(normalized);
          }
        }
      })
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, [conversationId, accessToken, activeBusinessId]);

  // Stable ref for the SSE-event handler shared by sendMessage.
  const onEventRef = useRef<(event: Record<string, unknown>) => void>(() => {});

  const handleSSEEvent = useCallback(
    (event: Record<string, unknown>) => {
      if (event.type === 'done') {
        // Invalidate using the business-scoped key prefix so React Query
        // refetches whichever conversation list is active (Plan 02-09).
        queryClient.invalidateQueries({
          queryKey: conversationsQueryKey(activeBusinessId),
        });
      }
      if (event.type === 'tool_approval_required') {
        const rawCalls = (event.calls as Array<Record<string, unknown>>) ?? [];
        const approval: PendingApproval = {
          batchId: event.batch_id as string,
          status: 'pending',
          createdAt: new Date().toISOString(),
          // expiresAt set by GET /messages hydration path, not SSE.
          calls: rawCalls.map((c) => ({
            callId: c.call_id as string,
            toolName: c.tool_name as string,
            args: (c.args as Record<string, unknown>) ?? {},
            editableFields: (c.editable_fields as string[]) ?? [],
            floor: c.floor as string,
          })),
        };
        onApprovalRequiredRef.current?.(approval);
        // Do NOT abort — orchestrator closes naturally; aborting masks errors.
        return;
      }
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (!last || last.role !== 'assistant') return prev;
        return [...prev.slice(0, -1), applySSEEvent(last, event)];
      });
    },
    [queryClient, activeBusinessId]
  );

  // Rebind on every render so any captured handler picks up the latest closure.
  useEffect(() => {
    onEventRef.current = handleSSEEvent;
  });

  // Force the last assistant message (if still in `streaming` state) into
  // `done`. Shared by sendMessage's finally-block (and re-used via the
  // synthetic `done` event the sibling resume flow fires through
  // appendSSEEvent) so a stream that closes without an explicit `done`
  // event — e.g., the HITL pause path on `tool_approval_required` or a
  // hung provider — still clears the typing indicator. No-op when the
  // last message is the user turn or already done.
  const finalizeStreamingAssistant = useCallback(() => {
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (!last || last.role !== 'assistant' || last.status !== 'streaming') return prev;
      return [...prev.slice(0, -1), { ...last, status: 'done' as const }];
    });
  }, []);

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
        const response = await fetch(chatUrl(activeBusinessId, conversationId), {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${accessToken}`,
          },
          body: JSON.stringify({ message: text }),
          signal: controller.signal,
        });

        await consumeSSEStream(response, controller.signal, onEventRef.current);
      } catch (error: unknown) {
        if ((error as Error).name === 'AbortError') return;
        setMessages((prev) => {
          const last = prev[prev.length - 1];
          if (!last || last.role !== 'assistant') return prev;
          return [
            ...prev.slice(0, -1),
            { ...last, content: tCommon('connectionError'), status: 'done' },
          ];
        });
      } finally {
        // Server-side closes the stream without a `done` event in two
        // legitimate cases: when emitting `tool_approval_required` (the
        // HITL pause) and when an upstream provider drops the connection
        // mid-run. Without forcing the message to `done` here the bubble
        // would show the typing indicator forever — flip it now and let
        // the resume stream re-flip back to streaming if it reopens.
        finalizeStreamingAssistant();
        setIsStreaming(false);
        isStreamingRef.current = false;
      }
    },
    [conversationId, accessToken, activeBusinessId, finalizeStreamingAssistant]
  );

  // appendSSEEvent — public for the sibling `usePendingApprovalFlow` to
  // forward resume-stream frames into the existing assistant message. The
  // resume stream's terminal 'done' replays the conversations invalidation;
  // 'tool_approval_required' is NOT replayed (resume never re-emits it).
  const appendSSEEvent = useCallback(
    (event: Record<string, unknown>) => {
      if (event.type === 'done') {
        queryClient.invalidateQueries({
          queryKey: conversationsQueryKey(activeBusinessId),
        });
      }
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (!last || last.role !== 'assistant') return prev;
        return [...prev.slice(0, -1), applySSEEvent(last, event)];
      });
    },
    [queryClient, activeBusinessId]
  );

  const stop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  return {
    messages,
    isLoading,
    isStreaming,
    sendMessage,
    stop,
    appendSSEEvent,
  };
}
