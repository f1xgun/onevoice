// v1.3 hardcoded tool catalogue. Refactored to a live API feed in Phase 16
// (POLICY-05) when the orchestrator exposes the full registered tool list.
//
// Consumers:
//   - services/frontend/components/projects/ToolCheckboxGrid.tsx (Plan 05)
//   - services/frontend/components/integrations/WhitelistWarningBanner.tsx (Plan 06)
//
// When the orchestrator registers new tools, update this map in one place.
export const TOOLS_BY_PLATFORM: Record<string, string[]> = {
  telegram: [
    'telegram__send_channel_post',
    'telegram__send_channel_photo',
    'telegram__send_notification',
  ],
  vk: [
    'vk__publish_post',
    'vk__post_photo',
    'vk__get_comments',
    'vk__reply_comment',
    'vk__delete_comment',
  ],
  yandex_business: [
    'yandex_business__get_reviews',
    'yandex_business__reply_review',
    'yandex_business__update_hours',
    'yandex_business__create_post',
  ],
  google_business: ['google_business__get_reviews', 'google_business__reply_review'],
};

export function toolsForPlatform(platform: string): string[] {
  return TOOLS_BY_PLATFORM[platform] ?? [];
}
