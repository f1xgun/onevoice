import { useState, useCallback, useRef, useEffect } from 'react';
import { toast } from 'sonner';
import { useAuthStore } from '@/lib/auth';
import { trackEvent } from '@/lib/telemetry';
import { resolveErrorToRussian, RESUME_STREAM_ERROR } from '@/lib/resolveErrorMap';
import type {
  ApprovalDecision,
  Message,
  PendingApproval,
  PendingApprovalCall,
  ToolCall,
} from '@/types/chat';

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

// consumeSSEStream is the ONE implementation of "read a fetch Response body
// as SSE and feed parsed events to onEvent" shared by both sendMessage and
// resolveApproval (the resume path). Keeping two copies caused divergence in
// error handling and abort semantics — 17-RESEARCH §Pattern 1.
async function consumeSSEStream(
  response: Response,
  signal: AbortSignal,
  onEvent: (event: Record<string, unknown>) => void
): Promise<void> {
  if (!response.ok || !response.body) throw new Error(`HTTP ${response.status}`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (!signal.aborted) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() ?? '';
    for (const line of lines) {
      const event = parseSSELine(line.trim());
      if (event) onEvent(event);
    }
  }
  if (buffer.trim()) {
    const event = parseSSELine(buffer.trim());
    if (event) onEvent(event);
  }
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

// Phase 16 GET /messages returns pendingApprovals already in camelCase, so
// this normalizer is effectively a typed cast + defensive defaults. It
// preserves status === 'expired' so the UI layer owns the render decision
// (Plan 17-05 `ExpiredApprovalBanner`, CONTEXT.md D-11).
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

export function useChat(conversationId: string) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const [pendingApproval, setPendingApproval] = useState<PendingApproval | null>(null);
  const isStreamingRef = useRef(false);
  const accessToken = useAuthStore((s) => s.accessToken);
  const abortRef = useRef<AbortController | null>(null);

  // Load existing messages on mount — accepts both legacy `ApiMessage[]` and
  // the Phase-16 `{messages, pendingApprovals}` envelope. When the envelope
  // carries a non-empty pendingApprovals array, hydrate the first batch so a
  // reloaded tab immediately shows the approval card (D-11).
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
        // Phase 17 hydration (D-11): surface the first persisted batch.
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
  }, [conversationId, accessToken]);

  // onEventRef keeps a fresh reference to the current SSE-event handler so
  // both sendMessage and resolveApproval can share one handler via
  // consumeSSEStream without recreating closures per call.
  const onEventRef = useRef<(event: Record<string, unknown>) => void>(() => {});

  const handleSSEEvent = useCallback((event: Record<string, unknown>) => {
    if (event.type === 'tool_approval_required') {
      const rawCalls = (event.calls as Array<Record<string, unknown>>) ?? [];
      setPendingApproval({
        batchId: event.batch_id as string,
        status: 'pending',
        createdAt: new Date().toISOString(),
        // expiresAt intentionally undefined — hydration path (GET /messages) carries it.
        calls: rawCalls.map((c) => ({
          callId: c.call_id as string,
          toolName: c.tool_name as string,
          args: (c.args as Record<string, unknown>) ?? {},
          editableFields: (c.editable_fields as string[]) ?? [],
          floor: c.floor as string,
        })),
      });
      // 17-RESEARCH §Pitfall 2: do NOT abort the controller here. The
      // orchestrator closes the response naturally after emitting the event;
      // aborting races with natural close and masks errors.
      return;
    }
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (!last || last.role !== 'assistant') return prev;
      return [...prev.slice(0, -1), applySSEEvent(last, event)];
    });
  }, []);

  // Rebind on every render so the resume stream picks up the latest closure.
  useEffect(() => {
    onEventRef.current = handleSSEEvent;
  });

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

        await consumeSSEStream(response, controller.signal, onEventRef.current);
      } catch (error: unknown) {
        if ((error as Error).name === 'AbortError') return;
        setMessages((prev) => {
          const last = prev[prev.length - 1];
          if (!last || last.role !== 'assistant') return prev;
          return [...prev.slice(0, -1), { ...last, content: 'Ошибка соединения', status: 'done' }];
        });
      } finally {
        setIsStreaming(false);
        isStreamingRef.current = false;
      }
    },
    [conversationId, accessToken]
  );

  const resolveApproval = useCallback(
    async (decisions: ApprovalDecision[]) => {
      if (!pendingApproval) return;
      if (isStreamingRef.current) return; // composer disabled should prevent this

      // Defensive sanitization at the trust boundary. Phase 16 D-09 pins the
      // toolName server-side; echoing the `tool_name` key signals misuse. We
      // strip it from edited_args and clamp reject_reason to 500 chars (D-08).
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
          copy.reject_reason = d.reject_reason.slice(0, 500);
        }
        return copy;
      });

      // 1) POST resolve — plain JSON per Phase 16 D-05.
      let resolveRes: Response;
      try {
        resolveRes = await fetch(
          `/api/v1/conversations/${conversationId}/pending-tool-calls/${pendingApproval.batchId}/resolve`,
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
        toast.error('Ошибка соединения — попробуйте ещё раз');
        return;
      }

      if (!resolveRes.ok) {
        let errBody: unknown = null;
        try {
          errBody = await resolveRes.json();
        } catch {
          // ignore parse failure — resolveErrorToRussian handles null body
        }
        toast.error(resolveErrorToRussian(resolveRes.status, errBody));
        return; // card stays open; Plan 17-04's ToolApprovalCard re-enables Submit.
      }

      // 2) Open the resume SSE — extends the existing assistant message
      //    (same message id; 17-RESEARCH §Pitfall 1).
      setIsStreaming(true);
      isStreamingRef.current = true;
      const controller = new AbortController();
      abortRef.current = controller;

      try {
        const resumeRes = await fetch(
          `/api/v1/chat/${conversationId}/resume?batch_id=${pendingApproval.batchId}`,
          {
            method: 'POST',
            headers: { Authorization: `Bearer ${accessToken}` },
            signal: controller.signal,
          }
        );
        await consumeSSEStream(resumeRes, controller.signal, onEventRef.current);
      } catch (err: unknown) {
        if ((err as Error).name === 'AbortError') return;
        toast.error(RESUME_STREAM_ERROR);
      } finally {
        // Phase 16 D-13: clear pendingApproval whether resume completed or
        // errored. The persisted batch on the server is the source of truth;
        // a reload re-hydrates from GET /messages.
        setPendingApproval(null);
        setIsStreaming(false);
        isStreamingRef.current = false;
      }
    },
    [conversationId, accessToken, pendingApproval]
  );

  const stop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  return {
    messages,
    isLoading,
    isStreaming,
    pendingApproval,
    resolveApproval,
    sendMessage,
    stop,
  };
}
