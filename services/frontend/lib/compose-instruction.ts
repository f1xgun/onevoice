// Pure builder for the guided-compose seed. The picker collects a post type,
// free-text topic, and confirmed destinations, then this turns them into a
// single templated instruction string handed to the existing chat send path
// (sendMessage). No producer, no draft state — just a string the chat loop
// already knows how to stream.

export type ComposePostType = 'announcement' | 'promo' | 'newArrival';
export type ComposeLocale = 'ru' | 'en';

export interface ComposeDestination {
  id: string;
  label: string;
}

export const COMPOSE_POST_TYPES: readonly ComposePostType[] = Object.freeze([
  'announcement',
  'promo',
  'newArrival',
]);

export const COMPOSE_PLATFORM_IDS = ['telegram', 'vk', 'yandex_business'] as const;

// Per-type instruction templates. The `{topic}` placeholder is replaced with
// the trimmed operator input. Kept in code (not messages/*.json) because the
// seed is a machine-facing instruction to the model, not user-facing copy —
// the visible labels/placeholders live under gettingStarted.compose.*.
const TEMPLATES: Record<ComposeLocale, Record<ComposePostType, string>> = {
  ru: {
    announcement: 'Напиши анонс для организации на тему: {topic}. Составь готовый пост.',
    promo: 'Составь пост об акции для организации: {topic}. Сделай текст завершённым.',
    newArrival: 'Расскажи о новинке в организации: {topic}. Подготовь готовый пост.',
  },
  en: {
    announcement:
      'Write an announcement for the organization about: {topic}. Prepare a complete post.',
    promo: 'Write a promotional post for the organization about: {topic}. Make it complete.',
    newArrival:
      'Introduce this new arrival for the organization: {topic}. Prepare a complete post.',
  },
};

const DESTINATION_DIRECTIVE: Record<ComposeLocale, (channels: string) => string> = {
  ru: (channels) =>
    `Опубликуй только в выбранных каналах: ${channels}. Не публикуй в других активных каналах. Подготовь отдельный адаптированный текст и один вызов инструмента публикации для каждого выбранного канала в одной группе подтверждения.`,
  en: (channels) =>
    `Publish only to these selected channels: ${channels}. Do not publish to any other active channel. Prepare a separately adapted draft and one publishing tool call for each selected channel in a single approval group.`,
};

export function isComposePostType(value: string): value is ComposePostType {
  return (COMPOSE_POST_TYPES as readonly string[]).includes(value);
}

// buildComposeInstruction returns the seeded instruction, or null when the
// topic is blank. Passing destinations makes the operator's concrete choice
// override the orchestrator's general broadcast-all directive.
export function buildComposeInstruction(
  type: ComposePostType,
  topic: string,
  destinations: readonly ComposeDestination[] = [],
  locale: ComposeLocale = 'ru'
): string | null {
  const trimmed = topic.trim();
  if (trimmed.length === 0) return null;
  const base = TEMPLATES[locale][type].replace('{topic}', trimmed);
  if (destinations.length === 0) return base;

  const channels = destinations.map(({ id, label }) => `${label} (${id})`).join(', ');
  return `${base} ${DESTINATION_DIRECTIVE[locale](channels)}`;
}
