import { describe, it, expect } from 'vitest';
import {
  buildComposeInstruction,
  isComposePostType,
  COMPOSE_POST_TYPES,
} from '@/lib/compose-instruction';

describe('buildComposeInstruction', () => {
  it('substitutes the trimmed topic into the announcement template', () => {
    expect(buildComposeInstruction('announcement', '  запуск нового меню  ')).toBe(
      'Напиши анонс для организации на тему: запуск нового меню. Составь готовый пост.'
    );
  });

  it('uses a distinct template per post type', () => {
    expect(buildComposeInstruction('promo', 'скидки')).toContain('акции');
    expect(buildComposeInstruction('newArrival', 'кофе')).toContain('новинке');
  });

  it('returns null for a blank topic so the caller keeps submit inert', () => {
    expect(buildComposeInstruction('announcement', '')).toBeNull();
    expect(buildComposeInstruction('announcement', '   ')).toBeNull();
  });

  it('makes concrete selected channels override the broadcast-all directive', () => {
    const instruction = buildComposeInstruction('announcement', 'новое меню', [
      { id: 'telegram', label: 'Telegram' },
      { id: 'vk', label: 'VK' },
    ]);

    expect(instruction).toContain('только в выбранных каналах: Telegram (telegram), VK (vk)');
    expect(instruction).toContain('Не публикуй в других активных каналах');
    expect(instruction).toContain('одной группе подтверждения');
    expect(instruction).not.toContain('yandex_business');
  });
});

describe('isComposePostType', () => {
  it('accepts every known post type and rejects others', () => {
    for (const t of COMPOSE_POST_TYPES) {
      expect(isComposePostType(t)).toBe(true);
    }
    expect(isComposePostType('unknown')).toBe(false);
  });
});
