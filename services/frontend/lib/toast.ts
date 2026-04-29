// lib/toast.ts — OneVoice (Linen) Sonner helpers
//
// Sonner is wired in `components/providers.tsx` with Linen-toned
// classNames. Don't replace it; this module only adds thin call-site
// helpers so the action-toast pattern from
// `design_handoff_onevoice 2/mocks/mock-states.jsx` (ErrorStatesPage,
// "Тост: что-то пошло не так") doesn't get re-implemented at every
// call-site.
//
// Brand voice: errors stay calm. Tell what happened, why it's not
// scary, what to do next. No "ой!", no exclamation marks, no raw
// stack traces.

import { toast as sonner } from 'sonner';

export interface ErrorWithActionOptions {
  /**
   * Plain-Russian explanation that shows under the headline. Optional
   * — short toasts can omit this and rely on the headline alone.
   */
  description?: string;
  /**
   * Forwarded to Sonner. Use a stable id when the same condition can
   * fire twice in a row so we don't stack identical toasts.
   */
  id?: string | number;
}

/**
 * Render an error toast that carries a single primary action — e.g.
 * "Telegram отклонил сообщение → [Разбить и отправить]". Wraps
 * `sonner.error` so call-sites stay one-liner.
 */
export function errorWithAction(
  message: string,
  actionLabel: string,
  onAction: () => void,
  options: ErrorWithActionOptions = {}
) {
  return sonner.error(message, {
    description: options.description,
    id: options.id,
    action: {
      label: actionLabel,
      onClick: onAction,
    },
  });
}

/**
 * Same idea, warning-toned. Used for non-blocking issues that the user
 * can fix in one click — e.g. "VK потерял авторизацию → [Переподключить]".
 */
export function warningWithAction(
  message: string,
  actionLabel: string,
  onAction: () => void,
  options: ErrorWithActionOptions = {}
) {
  return sonner.warning(message, {
    description: options.description,
    id: options.id,
    action: {
      label: actionLabel,
      onClick: onAction,
    },
  });
}

// Re-export the bare Sonner toast so callers can do
//   import { toast, errorWithAction } from '@/lib/toast';
// without juggling two imports.
export { sonner as toast };
