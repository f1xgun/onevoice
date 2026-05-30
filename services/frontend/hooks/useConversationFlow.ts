// useConversationFlow — single state machine for the chat conversation,
// merging the messages/streaming half (formerly useChat) and the HITL
// approval half (formerly usePendingApprovalFlow) that ChatWindow always
// consumed together. originally split these into sibling
// hooks for a "single writer for messages" invariant; in practice the
// split required ChatWindow to wire two bidirectional callbacks
// (chat.onApprovalRequired ↔ approvalFlow.setPending and
// approvalFlow.onResumeEvent ↔ chat.appendSSEEvent) plus a ref-bouncing
// trick to break the forward-reference between two sequentially-declared
// hooks — usePendingApprovalFlow already wrote to messages indirectly
// through appendSSEEvent, so single-writer was preserved on paper only.
// Collapsing back to one hook keeps the same observable contract while
// eliminating the wiring cost — there was only ever one consumer
// (ChatWindow) and one cohesive state machine.
//
// RBAC: all fetch URLs are business-scoped via the active business id
// from useBusinessStore. The conversation list invalidation uses
// conversationsQueryKey(activeBusinessId) so per-business cache
// partitioning stays intact across SSE 'done' events.

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

// Defensive cap on free-form reject reasons echoed to the server. The
// server enforces its own cap; this is the client-side mirror.
const REJECT_REASON_MAX_LEN = 500;

// Typed cast + defensive defaults. Preserves status === 'expired' so the
// UI layer owns the render decision (ExpiredApprovalBanner). Used by the
// GET /messages hydration path on mount.
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

// Business-scoped URL builders. Kept inline rather than centralised
// because every call site needs to forward the nullable activeBusinessId
// from the store and gracefully fall back to the legacy non-scoped path
// when no business is active.
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
}

interface ApiMessage {
  id: string;
  role: string;
  content: string;
  toolCalls?: ApiToolCall[];
  toolResults?: ApiToolResult[];
}

interface UseConversationFlowOptions {
  conversationId: string;
}

export function useConversationFlow({ conversationId }: UseConversationFlowOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const [pendingApproval, setPendingApproval] = useState<PendingApproval | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const isStreamingRef = useRef(false);
  const isResolvingRef = useRef(false);

  // Request-scoped translators. common.connectionError → assistant bubble
  // on stream errors; common.errors.connectionRetry → toast on resolve
  // network failure; resumeStreamError → toast on resume SSE failure.
  const tCommon = useTranslations('common');
  const tCommonErrors = useTranslations('common.errors');
  const { resolveError, resumeStreamError } = useResolveErrorMap();

  const accessToken = useAuthStore((s) => s.accessToken);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  // Separate abort controllers so that send and resolve/resume flows
  // never cancel each other. sendMessage uses its own controller, the
  // resume SSE inside resolveApproval uses its own.
  const sendAbortRef = useRef<AbortController | null>(null);
  const resumeAbortRef = useRef<AbortController | null>(null);
  // SSE 'done' invalidates conversationsQueryKey(activeBusinessId) for
  // out-of-band auto-title pickup. NEVER mux titles into chat SSE.
  const queryClient = useQueryClient();

  // applyEventToLastAssistant extends the in-place assistant message with
  // an SSE frame. Used by both the live-stream consumer (sendMessage) and
  // the resume-stream consumer (resolveApproval); the previous split
  // required exposing this as a public appendSSEEvent function so the
  // sibling hook could re-enter the messages state — now it's a single
  // closure with no public surface.
  const applyEventToLastAssistant = useCallback((event: Record<string, unknown>) => {
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (!last || last.role !== 'assistant') return prev;
      return [...prev.slice(0, -1), applySSEEvent(last, event)];
    });
  }, []);

  // Mount-load: legacy ApiMessage[] or {messages, pendingApprovals} envelope.
  // Sole /messages round trip; envelope's first batch flows into pendingApproval.
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
        // Hydration: surface the first persisted batch into pendingApproval.
        if (payload && !Array.isArray(payload)) {
          const pendings = (payload as { pendingApprovals?: unknown[] }).pendingApprovals;
          if (Array.isArray(pendings) && pendings.length > 0) {
            const normalized = normalizePendingApproval(pendings[0]);
            if (normalized) setPendingApproval(normalized);
          }
        }
      })
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, [conversationId, accessToken, activeBusinessId]);

  // handleChatSSEEvent — invariant for the LIVE send-message stream.
  // tool_approval_required flips pendingApproval and the goroutine on
  // the server closes naturally afterwards (the orchestrator already
  // persisted the batch).
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
          // expiresAt set by GET /messages hydration path, not SSE.
          calls: rawCalls.map((c) => ({
            callId: c.call_id as string,
            toolName: c.tool_name as string,
            args: (c.args as Record<string, unknown>) ?? {},
            editableFields: (c.editable_fields as string[]) ?? [],
            floor: c.floor as string,
          })),
        };
        setPendingApproval(approval);
        // Do NOT abort — orchestrator closes naturally; aborting masks errors.
        return;
      }
      applyEventToLastAssistant(event);
    },
    [queryClient, activeBusinessId, applyEventToLastAssistant]
  );

  // Stable ref so the send-message closure can dispatch to the latest
  // handler without being recreated when handleChatSSEEvent's identity
  // changes between renders.
  const onEventRef = useRef<(event: Record<string, unknown>) => void>(handleChatSSEEvent);
  useEffect(() => {
    onEventRef.current = handleChatSSEEvent;
  });

  // Force the last assistant message (if still in `streaming` state) into
  // `done`. Used by sendMessage's finally-block so a stream that closes
  // without an explicit `done` event (HITL pause path on
  // tool_approval_required or a hung provider) still clears the typing
  // indicator. No-op when the last message is the user turn or already
  // done.
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
        // Server-side closes the stream without a `done` event in two
        // legitimate cases: tool_approval_required (HITL pause) and a
        // hung upstream provider. Force the bubble out of streaming so
        // the typing indicator clears; the resume stream below will
        // re-flip back to streaming if it reopens.
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

  // resolveApproval — submit the user's per-call decisions to the API,
  // project synthetic rejection frames into the assistant message
  // immediately so rejected calls leave a visible trail, then open the
  // resume SSE that extends the same assistant message via
  // applyEventToLastAssistant. The persisted batch on the server is the
  // source of truth — a reload re-hydrates from GET /messages, so
  // pendingApproval is cleared on completion regardless of SSE outcome.
  const resolveApproval = useCallback(
    async (decisions: ApprovalDecision[]) => {
      if (!pendingApproval) return;
      if (isResolvingRef.current) return; // debounce — composer is also disabled

      // Defensive sanitization at the trust boundary. toolName is pinned
      // server-side; we never echo the `tool_name` key from edited_args.
      // reject_reason is clamped to 500 chars (server caps too).
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

      // 1) POST resolve — plain JSON.
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
        } catch {
          // ignore parse failure — resolveError handles null body
        }
        toast.error(resolveError(resolveRes.status, errBody));
        // card stays open; ToolApprovalCard re-enables Submit.
        isResolvingRef.current = false;
        setIsResolving(false);
        return;
      }

      // Project the user's rejection decisions into the assistant message
      // before the resume SSE opens so a rejected call leaves a visible
      // trail (red-bordered card with the "Отклонено пользователем" badge
      // and the operator's reason). Without this the rejected call would
      // live only in pendingApproval and vanish on clear — the empty
      // bubble would then be suppressed entirely, leaving no record at
      // all that a tool was invoked and refused. applySSEEvent dedupes
      // by tool_call_id when the server emits its own tool_rejected event
      // during resume. Only `reject` decisions are projected —
      // approve/edit calls produce their own real frames on the stream.
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

      // 2) Open the resume SSE — extends the existing assistant message
      // via applyEventToLastAssistant. Each `done` frame triggers the
      // same conversations cache invalidation as the live path.
      const controller = new AbortController();
      resumeAbortRef.current = controller;
      // Track whether the server emitted a real `done` so the finally
      // block doesn't double-fire one. The synthetic frame is a fallback
      // for the case where the resume stream closes after tool_rejected
      // without a terminal `done` event.
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
        // Clear pendingApproval whether resume completed or errored. The
        // persisted batch on the server is the source of truth; a reload
        // re-hydrates from GET /messages.
        setPendingApproval(null);
        if (!sawDone) {
          // Fallback: server closed without a terminal `done` (legit
          // after a synthetic tool_rejected). Flip the bubble out of
          // streaming so the typing indicator clears.
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
    sendMessage,
    stop,
    pendingApproval,
    resolveApproval,
    isResolving,
  };
}
