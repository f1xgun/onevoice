import { expect, it } from 'vitest';
import { businessMark, shortBusinessAddress } from '../business-display';
import { formatClockTime } from '../time-display';
import { getIntegrationDisplay } from '../integrations';

it('uses the trading name and keeps only street and house in navigation', () => {
  expect(businessMark('Кофейня «Зёрна»')).toBe('З');
  expect(businessMark('Acme Co')).toBe('A');
  expect(shortBusinessAddress('Екатеринбург, ул. Бажова, 68, вход со двора')).toBe(
    'ул. Бажова, 68'
  );
  expect(shortBusinessAddress('ул. Бажова, 68')).toBe('ул. Бажова, 68');
  expect(shortBusinessAddress('')).toBe('');
  expect(formatClockTime('08:00')).toBe('8:00');
  expect(formatClockTime('not a date')).toBe('');
});

it('shows recognizable metadata and never falls back to raw provider IDs', () => {
  for (const platform of ['telegram', 'vk', 'yandex_business']) {
    expect(getIntegrationDisplay({ platform, externalId: '-1002456128390' }, 'Площадка')).toEqual({
      name: 'Площадка',
      identifier: '',
    });
  }
  expect(
    getIntegrationDisplay(
      {
        platform: 'telegram',
        externalId: '-1002456128390',
        metadata: { channel_title: 'Зёрна на Бажова', channel_username: 'zerna_bazhova' },
      },
      'Telegram'
    )
  ).toEqual({ name: 'Зёрна на Бажова', identifier: '@zerna_bazhova' });
  expect(
    getIntegrationDisplay(
      {
        platform: 'vk',
        externalId: '225841396',
        metadata: { community_name: 'Кофейня Зёрна', members_count: 2340 },
      },
      'ВКонтакте'
    )
  ).toEqual({ name: 'Кофейня Зёрна', identifier: '', memberCount: 2340 });
});
