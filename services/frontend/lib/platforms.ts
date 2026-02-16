export const PLATFORM_COLORS: Record<string, string> = {
  vk: '#4680C2',
  telegram: '#2AABEE',
  yandex_business: '#FC3F1D',
}

export const PLATFORM_LABELS: Record<string, string> = {
  vk: 'VK',
  telegram: 'TG',
  yandex_business: 'YB',
}

export function getPlatform(toolName: string): string {
  return toolName.split('__')[0] ?? toolName
}
