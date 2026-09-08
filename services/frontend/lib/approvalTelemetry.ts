import { trackEvent } from '@/lib/telemetry';

export type ApprovalKind = 'post' | 'review_reply';
export type ApprovalSurface = 'chat' | 'reviews';
export type ApprovalClientAction = 'draft_shown' | 'approval_shown' | 'edit_saved';

const HEX_RADIX = 16;

/** Map only registered publication tools; tool names never enter telemetry. */
export function approvalKind(toolName: string): ApprovalKind | undefined {
  switch (toolName) {
    case 'telegram__send_channel_post':
    case 'telegram__send_channel_photo':
    case 'vk__publish_post':
    case 'vk__post_photo':
    case 'vk__schedule_post':
    case 'yandex_business__create_post':
      return 'post';
    case 'telegram__reply_to_comment':
    case 'vk__reply_comment':
    case 'yandex_business__reply_review':
    case 'google_business__reply_review':
      return 'review_reply';
    default:
      return undefined;
  }
}

/** Hash the dispatch identity, never draft text, before it reaches the buffer. */
export async function trackApprovalEvent(
  action: ApprovalClientAction,
  identity: string,
  kind: ApprovalKind,
  source: ApprovalSurface
): Promise<void> {
  try {
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(identity));
    const draftId = Array.from(new Uint8Array(digest), (byte) =>
      byte.toString(HEX_RADIX).padStart(2, '0')
    ).join('');
    trackEvent('approval', action, { metadata: { draft_id: draftId, kind, source } });
  } catch {}
}
