import { API_BASE_URL, API_PATHS } from '@/lib/constants/apiPaths';

import { useAuthStore } from './auth';
import { useBusinessStore } from './stores/business';

export interface TelemetryEvent {
  eventType: string;
  page: string;
  action: string;
  correlationId?: string;
  metadata?: Record<string, string>;
  timestamp: string;
}

const BATCH_INTERVAL = 5000; // 5 seconds
const MAX_BATCH_SIZE = 50;

interface BufferedTelemetry {
  event: TelemetryEvent;
  businessId: string | null;
}

let buffer: BufferedTelemetry[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * Track a frontend telemetry event. Events are batched and sent periodically.
 */
export function trackEvent(
  eventType: string,
  action: string,
  opts?: {
    page?: string;
    correlationId?: string;
    metadata?: Record<string, string>;
    businessId?: string | null;
  }
): void {
  if (!useAuthStore.getState().accessToken) return;

  const event: TelemetryEvent = {
    eventType,
    action,
    page: opts?.page ?? (typeof window !== 'undefined' ? window.location.pathname : ''),
    correlationId: opts?.correlationId,
    metadata: opts?.metadata,
    timestamp: new Date().toISOString(),
  };

  if (eventType === 'approval_server') return;
  if (eventType === 'approval') {
    const metadata = opts?.metadata;
    if (
      !['draft_shown', 'approval_shown', 'edit_saved'].includes(action) ||
      !/^[a-f0-9]{64}$/.test(metadata?.draft_id ?? '') ||
      !['post', 'review_reply'].includes(metadata?.kind ?? '') ||
      !['chat', 'reviews'].includes(metadata?.source ?? '')
    )
      return;
    event.metadata = {
      draft_id: metadata!.draft_id!,
      kind: metadata!.kind!,
      source: metadata!.source!,
    };
    event.correlationId = event.metadata.draft_id;
    event.page = `/${event.metadata.source}`;
  }

  buffer.push({
    event,
    businessId:
      eventType === 'approval'
        ? null
        : opts?.businessId === undefined
          ? (useBusinessStore.getState?.().activeBusinessId ?? null)
          : opts.businessId,
  });

  if (buffer.length >= MAX_BATCH_SIZE) {
    void flushTelemetry();
    return;
  }

  if (!flushTimer) {
    flushTimer = setTimeout(() => {
      flushTimer = null;
      void flushTelemetry();
    }, BATCH_INTERVAL);
  }
}

/**
 * Convenience wrapper for button_click events.
 */
export function trackClick(action: string, metadata?: Record<string, string>): void {
  trackEvent('button_click', action, { metadata });
}

/**
 * Flush all buffered telemetry events to the backend.
 * Fire-and-forget: errors are silently swallowed so telemetry never breaks the app.
 */
export async function flushTelemetry(): Promise<void> {
  await sendBufferedTelemetry(false);
}

/** Flush authenticated events on page hide without triggering auth navigation. */
export function flushOnHide(): void {
  void sendBufferedTelemetry(true);
}

async function sendBufferedTelemetry(keepalive: boolean): Promise<void> {
  const batch = buffer;
  buffer = [];
  if (flushTimer) {
    clearTimeout(flushTimer);
    flushTimer = null;
  }
  const token = useAuthStore.getState().accessToken;
  if (!token || batch.length === 0) return;
  const grouped = new Map<string | null, TelemetryEvent[]>();
  for (const item of batch) {
    const events = grouped.get(item.businessId) ?? [];
    events.push(item.event);
    grouped.set(item.businessId, events);
  }
  await Promise.all(
    [...grouped].map(async ([businessId, events]) => {
      const path = businessId
        ? `${API_BASE_URL}/businesses/${encodeURIComponent(businessId)}/telemetry`
        : `${API_BASE_URL}${API_PATHS.TELEMETRY}`;
      try {
        await fetch(path, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify(events),
          keepalive,
          credentials: 'include',
        });
      } catch {}
    })
  );
}

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      flushOnHide();
    }
  });
}
