// Pure builder for the guided-compose seed. The picker collects a post type,
// free-text topic, and confirmed destinations, then this turns them into a
// single templated instruction string handed to the existing chat send path
// (sendMessage). No producer, no draft state — just a string the chat loop
// already knows how to stream.

export type ComposePostType = 'announcement' | 'promo' | 'newArrival';

export interface ComposeDestination {
  id: string;
  label: string;
}

export const COMPOSE_POST_TYPES: readonly ComposePostType[] = Object.freeze([
  'announcement',
  'promo',
  'newArrival',
]);

// Per-type instruction templates. The `{topic}` placeholder is replaced with
// the trimmed operator input. Kept in code (not messages/*.json) because the
// seed is a machine-facing instruction to the model, not user-facing copy —
// the visible labels/placeholders live under gettingStarted.compose.*.
const TEMPLATES: Record<ComposePostType, string> = {
  announcement: 'Напиши анонс для организации на тему: {topic}. Составь готовый пост.',
  promo: 'Составь пост об акции для организации: {topic}. Сделай текст завершённым.',
  newArrival: 'Расскажи о новинке в организации: {topic}. Подготовь готовый пост.',
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
  destinations: readonly ComposeDestination[] = []
): string | null {
  const trimmed = topic.trim();
  if (trimmed.length === 0) return null;
  const base = TEMPLATES[type].replace('{topic}', trimmed);
  if (destinations.length === 0) return base;

  const channels = destinations.map(({ id, label }) => `${label} (${id})`).join(', ');
  return `${base} Опубликуй только в выбранных каналах: ${channels}. Не публикуй в других активных каналах. Подготовь отдельный адаптированный текст и один вызов инструмента публикации для каждого выбранного канала в одной группе подтверждения.`;
}
