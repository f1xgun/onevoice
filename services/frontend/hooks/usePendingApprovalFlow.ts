// usePendingApprovalFlow — owns the pendingApproval slice (Phase 19, plan
// 19-10, decision D-19). Sibling to `useChat`; both are consumed in parallel
// by ChatWindow. The two hooks NEVER share writable state — instead, the
// parent wires `useChat`'s `onApprovalRequired` callback to this hook's
// `setPending`, and this hook's `onResumeEvent` callback to `useChat`'s
// `appendSSEEvent`. That preserves a single writer for `messages` while
// keeping the pending-approval state cleanly testable in isolation.
//
// Phase 17 contracts preserved BYTE-IDENTICALLY:
// - `tool_name` strip from edited_args (security: server pins toolName)
// - `reject_reason.slice(0, 500)` clamp (security: bound payload)
// - hydration from GET /messages.pendingApprovals on mount
// - resolveApproval is no-op while a resolve is in flight (debounce)
// - resume SSE failure clears pendingApproval (orchestrator persists the batch)

import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';
import { useAuthStore } from '@/lib/auth';
import { useBusinessStore } from '@/lib/stores/business';
import { API_BASE_URL, API_STREAM_PATHS } from '@/lib/constants/apiPaths';
import { consumeSSEStream } from '@/lib/sse';
import { useResolveErrorMap } from '@/lib/resolveErrorMap';
import type { ApprovalDecision, PendingApproval } from '@/types/chat';

// Business-scoped URL builders (RBAC plan 02-09). Falls back to the legacy
// non-scoped path when no business is active so unit tests that do not mock
// `useBusinessStore` still hit the pre-RBAC URLs the API_STREAM_PATHS
// helpers produce.
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

// Defensive cap: we never echo more than 500 chars of free-form reject
// reason to the server — a server-side cap exists too, but the trust
// boundary is mirrored here.
const REJECT_REASON_MAX_LEN = 500;

interface UsePendingApprovalFlowOptions {
  conversationId: string;
  // Wired by the parent component to `useChat`'s `appendSSEEvent`. Each
  // resume-stream SSE frame is forwarded so the existing assistant message
  // (owned by useChat's `messages` state) extends in place.
  onResumeEvent: (event: Record<string, unknown>) => void;
}

export function usePendingApprovalFlow({
  conversationId,
  onResumeEvent,
}: UsePendingApprovalFlowOptions) {
  const [pendingApproval, setPendingApproval] = useState<PendingApproval | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const isResolvingRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const accessToken = useAuthStore((s) => s.accessToken);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  // Request-scoped error map (Phase B1). useChat keeps its own
  // `connectionError` lookup; the resolve/resume branches here used to
  // import resolveErrorToRussian + RESUME_STREAM_ERROR from a module
  // that pinned ru.json at load time.
  const tCommonErrors = useTranslations('common.errors');
  const { resolveError, resumeStreamError } = useResolveErrorMap();

  // onResumeEventRef keeps a fresh reference to the current resume-event
  // handler so the SSE consumer below can dispatch via a stable function
  // identity without re-creating the resolveApproval closure when the
  // parent's appendSSEEvent identity changes between renders.
  const onResumeEventRef = useRef<(event: Record<string, unknown>) => void>(onResumeEvent);
  useEffect(() => {
    onResumeEventRef.current = onResumeEvent;
  });

  // setPending is the single ingress for both SSE arrival (`useChat`'s
  // `onApprovalRequired` callback) and hydration replay (also routed
  // through `useChat`'s GET /messages handler — `useChat` is the sole
  // fetcher of the messages endpoint, preserving the single round-trip
  // invariant from before the hook split).
  const setPending = useCallback((approval: PendingApproval) => {
    setPendingApproval(approval);
  }, []);

  const resolveApproval = useCallback(
    async (decisions: ApprovalDecision[]) => {
      if (!pendingApproval) return;
      if (isResolvingRef.current) return; // debounce — composer is also disabled

      // Defensive sanitization at the trust boundary. The toolName is pinned
      // server-side; echoing the `tool_name` key signals misuse. We strip it
      // from edited_args and clamp reject_reason to 500 chars.
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
      // all that a tool was invoked and refused.
      //
      // Synthetic frames are routed through the same `onResumeEvent`
      // callback the real SSE stream uses; `applySSEEvent` then dedupes
      // by tool_call_id when the server emits its own `tool_rejected`
      // event during resume. Only `reject` decisions are projected —
      // approve/edit calls produce their own real frames on the stream.
      for (const c of pendingApproval.calls) {
        const dec = sanitizedDecisions.find((d) => d.id === c.callId);
        if (dec?.action !== 'reject') continue;
        onResumeEventRef.current({
          type: 'tool_rejected',
          tool_call_id: c.callId,
          tool_name: c.toolName,
          content: dec.reject_reason ?? '',
          args: c.args,
        });
      }

      // 2) Open the resume SSE — extends the existing assistant message
      //    (owned by useChat). Each frame goes via onResumeEventRef →
      //    chat.appendSSEEvent.
      const controller = new AbortController();
      abortRef.current = controller;
      // Track whether the server emitted a real `done` so the finally
      // block doesn't double-fire one. The synthetic frame is a fallback
      // for the case where the resume stream closes after tool_rejected
      // without a terminal `done` event — without it the bubble keeps
      // showing the typing indicator forever.
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
          if (event.type === 'done') sawDone = true;
          onResumeEventRef.current(event);
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
          // Fallback: server closed the stream without a terminal `done`
          // (legitimate after a synthetic tool_rejected). Flip the bubble
          // out of `streaming` so the typing indicator clears.
          onResumeEventRef.current({ type: 'done' });
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
    ]
  );

  return {
    pendingApproval,
    setPending,
    resolveApproval,
    isResolving,
  };
}
