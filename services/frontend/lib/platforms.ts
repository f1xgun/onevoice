export const PLATFORM_COLORS: Record<string, string> = {
  vk: '#4680C2',
  telegram: '#2AABEE',
  yandex_business: '#FC3F1D',
  google_business: '#1A73E8',
};

export const PLATFORM_LABELS: Record<string, string> = {
  vk: 'VK',
  telegram: 'TG',
  yandex_business: 'YB',
  google_business: 'GB',
};

// Full human-readable platform names (used for whitelist grouping headers)
export const PLATFORM_FULL_LABELS: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'ВКонтакте',
  yandex_business: 'Яндекс.Бизнес',
  google_business: 'Google Business',
};

export function getPlatform(toolName: string): string {
  return toolName.split('__')[0] ?? toolName;
}
