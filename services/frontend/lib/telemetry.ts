import { api } from './api';

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

let buffer: TelemetryEvent[] = [];
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
  }
): void {
  const event: TelemetryEvent = {
    eventType,
    action,
    page: opts?.page ?? (typeof window !== 'undefined' ? window.location.pathname : ''),
    correlationId: opts?.correlationId,
    metadata: opts?.metadata,
    timestamp: new Date().toISOString(),
  };

  buffer.push(event);

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
  if (buffer.length === 0) return;

  const batch = buffer;
  buffer = [];

  if (flushTimer) {
    clearTimeout(flushTimer);
    flushTimer = null;
  }

  try {
    await api.post('/telemetry', batch);
  } catch {
    // Silently swallow — telemetry must never break the app
  }
}

// On page hide, flush remaining events via sendBeacon (works during unload)
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden' && buffer.length > 0) {
      const batch = buffer;
      buffer = [];

      if (flushTimer) {
        clearTimeout(flushTimer);
        flushTimer = null;
      }

      const blob = new Blob([JSON.stringify(batch)], {
        type: 'application/json',
      });
      navigator.sendBeacon('/api/v1/telemetry', blob);
    }
  });
}
