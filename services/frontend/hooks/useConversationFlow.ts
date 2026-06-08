// Single state machine for the chat conversation: messages/streaming and
// HITL approval. All fetch URLs are business-scoped via activeBusinessId;
// conversations cache is partitioned by business via conversationsQueryKey.

import { useState, useCallback, useRef, useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { API_BASE_URL, API_STREAM_PATHS } from '@/lib/constants/apiPaths';
import { applySSEEvent, consumeSSEStream } from '@/lib/sse';
import { trackEvent } from '@/lib/telemetry';
import { useResolveErrorMap } from '@/lib/resolveErrorMap';
import type {
  ApprovalDecision,
  Message,
  PendingApproval,
  PendingApprovalCall,
  ToolCall,
} from '@/types/chat';

// Client-side mirror of the server's reject-reason cap.
const REJECT_REASON_MAX_LEN = 500;

// Synthetic placeholder ID for the "still generating" assistant bubble shown
// after a reload that lands mid-turn (see resumeInFlightTurn). Sentinel, never
// persisted — replaced by the real assistant message once the server finishes.
const AWAITING_TURN_MESSAGE_ID = '__onevoice_awaiting_turn__';

// The server finishes and persists a turn even after the client disconnects
// (the API drains the orchestrator stream on a detached context — see
// services/api/internal/service/chatturn/stream.go). So on reload mid-turn we
// poll GET /messages until the assistant reply lands. Interval / cap below;
// the cap matches the orchestrator streamBudget (10 min) so a long RPA chain
// is not abandoned early.
const TURN_POLL_INTERVAL_MS = 3000;
const TURN_POLL_MAX_ATTEMPTS = 200;

// Preserves status === 'expired' so the UI owns the render decision
// (ExpiredApprovalBanner).
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
  const status: PendingApproval['status'] =
    r.status === 'expired' ? 'expired' : r.status === 'resolving' ? 'resolving' : 'pending';
  return {
    batchId: (r.batchId as string) ?? '',
    conversationId: r.conversationId as string | undefined,
    status,
    calls,
    expiresAt: r.expiresAt as string | undefined,
    createdAt: (r.createdAt as string) ?? new Date().toISOString(),
  };
}

// Business-scoped URL builders fall back to the non-scoped path when no
// business is active.
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

function pendingResolveUrl(
  activeBusinessId: string | null,
  conversationId: string,
  batchId: string
): string {
  return activeBusinessId
    ? `${API_BASE_URL}/businesses/${activeBusinessId}/conversations/${conversationId}/pending-tool-calls/${batchId}/resolve`
    : API_STREAM_PATHS.PENDING_TOOL_CALLS_RESOLVE(conversationId, batchId);
}

function chatResumeUrl(
  activeBusinessId: string | null,
  conversationId: string,
  batchId: string
): string {
  return activeBusinessId
    ? `${API_BASE_URL}/businesses/${activeBusinessId}/chat/${conversationId}/resume?batch_id=${batchId}`
    : API_STREAM_PATHS.CHAT_RESUME(conversationId, batchId);
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
  code?: string;
}

interface ApiMessage {
  id: string;
  role: string;
  content: string;
  toolCalls?: ApiToolCall[];
  toolResults?: ApiToolResult[];
}

type MessagesPayload = ApiMessage[] | { messages: ApiMessage[]; pendingApprovals?: unknown[] };

// extractApiMessages normalizes both wire shapes (bare array, or
// { messages, pendingApprovals }) to ApiMessage[] | null.
function extractApiMessages(payload: MessagesPayload | null): ApiMessage[] | null {
  if (Array.isArray(payload)) return payload;
  if (payload && Array.isArray((payload as { messages?: ApiMessage[] }).messages)) {
    return (payload as { messages: ApiMessage[] }).messages;
  }
  return null;
}

// mapApiMessages converts persisted API messages to the client Message shape.
// Every loaded message is status:'done' — the live SSE stream is what produces
// 'streaming' messages.
function mapApiMessages(apiMsgs: ApiMessage[]): Message[] {
  return apiMsgs.map((m) => {
    const toolCalls: ToolCall[] | undefined =
      m.toolCalls && m.toolCalls.length > 0
        ? m.toolCalls.map((tc) => {
            const result = m.toolResults?.find((r) => r.toolCallId === tc.id);
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
              error: result?.isError ? ((result.content?.error as string) ?? 'error') : undefined,
              code: (result?.code as ToolCall['code']) || undefined,
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
  });
}

interface UseConversationFlowOptions {
  conversationId: string;
}

export function useConversationFlow({ conversationId }: UseConversationFlowOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const [awaitingTurn, setAwaitingTurn] = useState(false);
  const [pendingApproval, setPendingApproval] = useState<PendingApproval | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const isStreamingRef = useRef(false);
  const isResolvingRef = useRef(false);
  const turnPollRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const tCommon = useTranslations('common');
  const tCommonErrors = useTranslations('common.errors');
  const { resolveError, resumeStreamError } = useResolveErrorMap();

  const accessToken = useAuthStore((s) => s.accessToken);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const sendAbortRef = useRef<AbortController | null>(null);
  const resumeAbortRef = useRef<AbortController | null>(null);
  const queryClient = useQueryClient();

  const applyEventToLastAssistant = useCallback((event: Record<string, unknown>) => {
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (!last || last.role !== 'assistant') return prev;
      return [...prev.slice(0, -1), applySSEEvent(last, event)];
    });
  }, []);

  // Load history, then keep polling while a turn is still being generated
  // server-side. The server finishes and persists a turn even after the client
  // disconnects (refresh), so a trailing user message with no assistant reply
  // means "still generating" — we show the typing placeholder and poll GET
  // /messages until the reply lands (or the attempt cap is hit).
  useEffect(() => {
    let cancelled = false;
    let attempts = 0;
    setIsLoading(true);
    setAwaitingTurn(false);

    const clearPoll = () => {
      if (turnPollRef.current) {
        clearTimeout(turnPollRef.current);
        turnPollRef.current = null;
      }
    };

    const load = async (isInitial: boolean): Promise<void> => {
      let payload: MessagesPayload | null = null;
      try {
        const r = await fetch(messagesUrl(activeBusinessId, conversationId), {
          headers: { Authorization: `Bearer ${accessToken}` },
        });
        payload = r.ok ? ((await r.json()) as MessagesPayload) : null;
      } catch {
        payload = null;
      }
      if (cancelled) return;
      // A live send took over (only possible via a race) — let the SSE stream
      // own message state instead of clobbering it with the persisted list.
      if (!isInitial && isStreamingRef.current) {
        clearPoll();
        setAwaitingTurn(false);
        return;
      }

      const apiMsgs = extractApiMessages(payload);
      const pendings =
        payload && !Array.isArray(payload)
          ? (payload as { pendingApprovals?: unknown[] }).pendingApprovals
          : undefined;
      const hasPending = Array.isArray(pendings) && pendings.length > 0;

      let keepWaiting = false;
      if (apiMsgs) {
        const mapped = mapApiMessages(apiMsgs);
        const last = mapped[mapped.length - 1];
        const turnInFlight =
          !hasPending && !isStreamingRef.current && !!last && last.role === 'user';
        keepWaiting = turnInFlight && attempts < TURN_POLL_MAX_ATTEMPTS;
        if (keepWaiting) {
          mapped.push({
            id: AWAITING_TURN_MESSAGE_ID,
            role: 'assistant',
            content: '',
            toolCalls: [],
            status: 'streaming',
          });
        }
        setMessages(mapped);
      } else if (!isInitial) {
        // Transient GET failure mid-poll — keep the placeholder and retry up to
        // the cap instead of dropping the indicator on a network blip.
        keepWaiting = attempts < TURN_POLL_MAX_ATTEMPTS;
      }

      if (hasPending) {
        const normalized = normalizePendingApproval((pendings as unknown[])[0]);
        if (normalized) setPendingApproval(normalized);
      }

      setAwaitingTurn(keepWaiting);
      if (keepWaiting) {
        attempts += 1;
        turnPollRef.current = setTimeout(() => void load(false), TURN_POLL_INTERVAL_MS);
      } else {
        clearPoll();
        // A turn we were waiting on just resolved — refresh the conversation
        // list so the auto-generated title appears without a manual reload.
        if (attempts > 0 && apiMsgs) {
          queryClient.invalidateQueries({ queryKey: conversationsQueryKey(activeBusinessId) });
        }
      }

      if (isInitial) setIsLoading(false);
    };

    void load(true);

    return () => {
      cancelled = true;
      clearPoll();
    };
  }, [conversationId, accessToken, activeBusinessId, queryClient]);

  const handleChatSSEEvent = useCallback(
    (event: Record<string, unknown>) => {
      if (event.type === 'done') {
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
          calls: rawCalls.map((c) => ({
            callId: c.call_id as string,
            toolName: c.tool_name as string,
            args: (c.args as Record<string, unknown>) ?? {},
            editableFields: (c.editable_fields as string[]) ?? [],
            floor: c.floor as string,
          })),
        };
        setPendingApproval(approval);
        return;
      }
      applyEventToLastAssistant(event);
    },
    [queryClient, activeBusinessId, applyEventToLastAssistant]
  );

  const onEventRef = useRef<(event: Record<string, unknown>) => void>(handleChatSSEEvent);
  useEffect(() => {
    onEventRef.current = handleChatSSEEvent;
  });

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

      // Stop any in-flight-turn polling and drop its placeholder — the live
      // stream now owns message state.
      if (turnPollRef.current) {
        clearTimeout(turnPollRef.current);
        turnPollRef.current = null;
      }
      setAwaitingTurn(false);

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

      setMessages((prev) => [
        ...prev.filter((m) => m.id !== AWAITING_TURN_MESSAGE_ID),
        userMessage,
        assistantMessage,
      ]);
      setIsStreaming(true);
      isStreamingRef.current = true;

      trackEvent('chat_send', 'send_message', {
        metadata: { conversationId: conversationId ?? '' },
      });

      const controller = new AbortController();
      sendAbortRef.current = controller;

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
        finalizeStreamingAssistant();
        setIsStreaming(false);
        isStreamingRef.current = false;
      }
    },
    [conversationId, accessToken, activeBusinessId, finalizeStreamingAssistant, tCommon]
  );

  const stop = useCallback(() => {
    sendAbortRef.current?.abort();
  }, []);

  const resolveApproval = useCallback(
    async (decisions: ApprovalDecision[]) => {
      if (!pendingApproval) return;
      if (isResolvingRef.current) return; // debounce — composer is also disabled

      const sanitizedDecisions: ApprovalDecision[] = decisions.map((d) => {
        const copy: ApprovalDecision = { id: d.id, action: d.action };
        if (d.action === 'edit' && d.edited_args) {
          const filtered: Record<string, string | number | boolean> = {};
          for (const [k, v] of Object.entries(d.edited_args)) {
            if (k === 'tool_name') continue; // NEVER echo
            filtered[k] = v;
          }
          copy.edited_args = filtered;
        }
        if (d.action === 'reject' && d.reject_reason !== undefined) {
          copy.reject_reason = d.reject_reason.slice(0, REJECT_REASON_MAX_LEN);
        }
        return copy;
      });

      isResolvingRef.current = true;
      setIsResolving(true);

      let resolveRes: Response;
      try {
        resolveRes = await fetch(
          pendingResolveUrl(activeBusinessId, conversationId, pendingApproval.batchId),
          {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              Authorization: `Bearer ${accessToken}`,
            },
            body: JSON.stringify({ decisions: sanitizedDecisions }),
          }
        );
      } catch {
        toast.error(tCommonErrors('connectionRetry'));
        isResolvingRef.current = false;
        setIsResolving(false);
        return;
      }

      if (!resolveRes.ok) {
        let errBody: unknown = null;
        try {
          errBody = await resolveRes.json();
        } catch {}
        toast.error(resolveError(resolveRes.status, errBody));
        isResolvingRef.current = false;
        setIsResolving(false);
        return;
      }

      for (const c of pendingApproval.calls) {
        const dec = sanitizedDecisions.find((d) => d.id === c.callId);
        if (dec?.action !== 'reject') continue;
        applyEventToLastAssistant({
          type: 'tool_rejected',
          tool_call_id: c.callId,
          tool_name: c.toolName,
          content: dec.reject_reason ?? '',
          args: c.args,
        });
      }

      const controller = new AbortController();
      resumeAbortRef.current = controller;
      let sawDone = false;

      try {
        const resumeRes = await fetch(
          chatResumeUrl(activeBusinessId, conversationId, pendingApproval.batchId),
          {
            method: 'POST',
            headers: { Authorization: `Bearer ${accessToken}` },
            signal: controller.signal,
          }
        );
        await consumeSSEStream(resumeRes, controller.signal, (event) => {
          if (event.type === 'done') {
            sawDone = true;
            queryClient.invalidateQueries({
              queryKey: conversationsQueryKey(activeBusinessId),
            });
          }
          applyEventToLastAssistant(event);
        });
      } catch (err: unknown) {
        if ((err as Error).name === 'AbortError') return;
        toast.error(resumeStreamError);
      } finally {
        setPendingApproval(null);
        if (!sawDone) {
          applyEventToLastAssistant({ type: 'done' });
        }
        isResolvingRef.current = false;
        setIsResolving(false);
      }
    },
    [
      conversationId,
      accessToken,
      activeBusinessId,
      pendingApproval,
      tCommonErrors,
      resolveError,
      resumeStreamError,
      queryClient,
      applyEventToLastAssistant,
    ]
  );

  return {
    messages,
    isLoading,
    isStreaming,
    awaitingTurn,
    sendMessage,
    stop,
    pendingApproval,
    resolveApproval,
    isResolving,
  };
}
