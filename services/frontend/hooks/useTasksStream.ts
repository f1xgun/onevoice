'use client';

import { useEffect, useRef } from 'react';
import { useAuthStore } from '@/lib/auth';
import { authFetch } from '@/lib/api/authFetch';
import { useBusinessStore } from '@/lib/stores/business';
import type { TaskStreamEvent } from '@/types/task';

const reconnectDelayMs = 2_000;
const maxReconnectDelayMs = 30_000;
const SSE_DATA_PREFIX = 'data: ';

/**
 * useTasksStream subscribes to the SSE endpoint /api/v1/tasks/stream and
 * invokes onEvent for every task.created / task.updated event. Reconnects
 * on any terminated connection (server restart, network flap) with
 * exponential backoff (capped), reset once a connection is established. The
 * request goes through authFetch, so an access token that expired while the
 * stream was open triggers a single token refresh + replay instead of a
 * tight 401 reconnect loop against a dead token.
 */
export function useTasksStream(onEvent: (ev: TaskStreamEvent) => void) {
  const accessToken = useAuthStore((s) => s.accessToken);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  useEffect(() => {
    if (!accessToken || !activeBusinessId) return;

    let cancelled = false;
    let controller: AbortController | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let reconnectAttempt = 0;

    async function connect() {
      if (cancelled) return;
      controller = new AbortController();
      try {
        const response = await authFetch(`/api/v1/businesses/${activeBusinessId}/tasks/stream`, {
          signal: controller.signal,
        });
        if (!response.ok || !response.body) {
          throw new Error(`HTTP ${response.status}`);
        }
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        while (!cancelled) {
          const { done, value } = await reader.read();
          if (done) break;
          // Receiving any bytes (an event or a heartbeat ping) means the
          // stream is genuinely live — only now reset the reconnect backoff,
          // so an accept-then-immediate-drop keeps backing off instead of
          // hot-looping every 2s.
          reconnectAttempt = 0;
          buffer += decoder.decode(value, { stream: true });
          const chunks = buffer.split('\n\n');
          buffer = chunks.pop() ?? '';
          for (const chunk of chunks) {
            const dataLine = chunk.split('\n').find((l) => l.startsWith(SSE_DATA_PREFIX));
            if (!dataLine) continue; // skip ': ping' heartbeats
            try {
              const parsed = JSON.parse(dataLine.slice(SSE_DATA_PREFIX.length)) as TaskStreamEvent;
              onEventRef.current(parsed);
            } catch {}
          }
        }
      } catch (err) {
        if (cancelled || (err as Error).name === 'AbortError') return;
      }
      if (!cancelled) {
        const delay = Math.min(reconnectDelayMs * 2 ** reconnectAttempt, maxReconnectDelayMs);
        reconnectAttempt += 1;
        reconnectTimer = setTimeout(connect, delay);
      }
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      controller?.abort();
    };
  }, [accessToken, activeBusinessId]);
}
