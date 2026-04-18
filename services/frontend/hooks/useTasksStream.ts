'use client';

import { useEffect, useRef } from 'react';
import { useAuthStore } from '@/lib/auth';
import type { TaskStreamEvent } from '@/types/task';

const reconnectDelayMs = 2_000;

/**
 * useTasksStream subscribes to the SSE endpoint /api/v1/tasks/stream and
 * invokes onEvent for every task.created / task.updated event. Reconnects
 * on any terminated connection (server restart, network flap) after a
 * short delay.
 */
export function useTasksStream(onEvent: (ev: TaskStreamEvent) => void) {
  const accessToken = useAuthStore((s) => s.accessToken);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  useEffect(() => {
    if (!accessToken) return;

    let cancelled = false;
    let controller: AbortController | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    async function connect() {
      if (cancelled) return;
      controller = new AbortController();
      try {
        const response = await fetch('/api/v1/tasks/stream', {
          headers: { Authorization: `Bearer ${accessToken}` },
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
          buffer += decoder.decode(value, { stream: true });
          const chunks = buffer.split('\n\n');
          buffer = chunks.pop() ?? '';
          for (const chunk of chunks) {
            const dataLine = chunk.split('\n').find((l) => l.startsWith('data: '));
            if (!dataLine) continue; // skip ': ping' heartbeats
            try {
              const parsed = JSON.parse(dataLine.slice(6)) as TaskStreamEvent;
              onEventRef.current(parsed);
            } catch {
              // malformed event — ignore
            }
          }
        }
      } catch (err) {
        if (cancelled || (err as Error).name === 'AbortError') return;
      }
      if (!cancelled) {
        reconnectTimer = setTimeout(connect, reconnectDelayMs);
      }
    }

    connect();

    return () => {
      cancelled = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      controller?.abort();
    };
  }, [accessToken]);
}
