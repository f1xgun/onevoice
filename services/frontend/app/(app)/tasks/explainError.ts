// Pure error explainer for the tasks page. Lives in its own module so
// the explainError logic can be unit-tested without pulling in the
// page.tsx client tree (next-intl + React Query + Zustand hooks).
//
// Brand Voice Guide §3: explain what + why + what-to-do-next, calmly,
// no exclamation marks. We never surface the raw error string.
//
// explainError switches on the typed `errorCode` stamped by the platform
// agent's classifier — the locked enum carried end-to-end:
//   integration_token_invalid, rate_limit_exceeded, transient,
//   channel_not_found, media_too_large.
// Historical rows (no errorCode) and unknown codes fall through to the
// calm fallback summary.

import { API_PATHS } from '@/lib/constants/apiPaths';
import { reconnectLabelKey, tokenErrorKey } from '@/lib/platforms';
import type { AgentTask } from '@/types/task';

export interface HumanError {
  summaryKey: string;
  cta?: { labelKey: string; href: string };
  willAutoRetry?: boolean;
}

export function explainError(task: AgentTask): HumanError {
  const platform = task.platform;

  switch (task.errorCode) {
    case 'integration_token_invalid': {
      const summaryKey = tokenErrorKey(platform);
      const labelKey = reconnectLabelKey(platform);
      const href = platform
        ? `${API_PATHS.INTEGRATIONS.ROOT}?reconnect=${platform}`
        : API_PATHS.INTEGRATIONS.ROOT;
      return { summaryKey, cta: { labelKey, href } };
    }
    case 'rate_limit_exceeded':
      return { summaryKey: 'rateLimit', willAutoRetry: false };
    case 'transient':
      return { summaryKey: 'transient', willAutoRetry: false };
    case 'channel_not_found':
      return {
        summaryKey: 'notFound',
        cta: { labelKey: 'openIntegrations', href: API_PATHS.INTEGRATIONS.ROOT },
      };
    case 'media_too_large':
      return { summaryKey: 'media' };
    default:
      return { summaryKey: 'fallback', willAutoRetry: false };
  }
}
