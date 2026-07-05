// Pure builder for the guided-compose seed. The picker collects a post type
// and a free-text topic, then this turns them into a single templated
// instruction string that is handed to the existing chat send path
// (sendMessage). No producer, no draft state — just a string the chat loop
// already knows how to stream.

export type ComposePostType = 'announcement' | 'promo' | 'newArrival';

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
// topic is blank so the caller can keep the submit affordance inert.
export function buildComposeInstruction(type: ComposePostType, topic: string): string | null {
  const trimmed = topic.trim();
  if (trimmed.length === 0) return null;
  return TEMPLATES[type].replace('{topic}', trimmed);
}
