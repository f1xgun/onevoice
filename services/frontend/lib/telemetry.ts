import { useAuthStore } from './auth';
import { API_BASE_URL, API_PATHS } from '@/lib/constants/apiPaths';

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
  if (!useAuthStore.getState().accessToken) return;

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
  try {
    await fetch(`${API_BASE_URL}${API_PATHS.TELEMETRY}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(batch),
      keepalive,
      credentials: 'include',
    });
  } catch {}
}

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      flushOnHide();
    }
  });
}
