import { api } from './api';
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
    await api.post(API_PATHS.TELEMETRY, batch);
  } catch {}
}

/**
 * Flush buffered events on page hide. Uses fetch with `keepalive` — which, like
 * sendBeacon, survives unload — but unlike sendBeacon can attach the
 * Authorization header. `POST /telemetry` is JWT-protected, so a header-less
 * sendBeacon 401'd and silently dropped every page-hide batch.
 *
 * With no access token the events can't be attributed, so the buffer is left
 * intact for a later authenticated flush rather than dropped.
 */
export function flushOnHide(): void {
  if (buffer.length === 0) return;

  const token = useAuthStore.getState().accessToken;
  if (!token) return;

  const batch = buffer;
  buffer = [];

  if (flushTimer) {
    clearTimeout(flushTimer);
    flushTimer = null;
  }

  void fetch(`${API_BASE_URL}${API_PATHS.TELEMETRY}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(batch),
    keepalive: true,
    credentials: 'include',
  }).catch(() => {});
}

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      flushOnHide();
    }
  });
}
