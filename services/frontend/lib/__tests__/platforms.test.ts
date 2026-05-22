import { describe, expect, it } from 'vitest';

import {
  PLATFORM_STATIC_META,
  PLATFORM_DISPLAY_ORDER,
  PLATFORM_COLORS,
  PLATFORM_LABELS,
  createPlatformFullLabels,
  createPlatformMeta,
  isKnownPlatform,
  type PlatformId,
} from '../platforms';

// The Go registry in pkg/domain/platform.go is the authority for which
// platforms exist and their at-rest status. PLATFORM_STATIC_META is the
// frontend shadow that fills in presentation. These tests pin the parity
// invariant so adding a platform on one side without the other fails
// fast — rather than silently rendering a connect card for a non-MVP
// platform when the React Query fetch races UI render (and therefore
// returns the static fallback first).
//
// Update this list whenever pkg/domain/platform.go grows or shrinks.
const REGISTRY_IDS_FROM_GO: ReadonlyArray<PlatformId> = [
  'telegram',
  'vk',
  'yandex_business',
  'google_business',
  '2gis',
  'avito',
  'whatsapp',
];

const REGISTRY_DEFAULT_STATUS: ReadonlyMap<PlatformId, 'active' | 'coming_soon'> = new Map([
  ['telegram', 'active'],
  ['vk', 'active'],
  ['yandex_business', 'active'],
  ['google_business', 'coming_soon'],
  ['2gis', 'coming_soon'],
  ['avito', 'coming_soon'],
  ['whatsapp', 'coming_soon'],
]);

// Stub translators — mirror what `useTranslations('platforms.fullLabel')`
// / `useTranslations('platforms')` produce. We're testing the shape
// invariants, not the localized copy.
const tFullLabel = (id: PlatformId) => `label:${id}`;
const tPlatforms = (key: 'comingSoonWhen') => `coming-soon-when:${key}`;

describe('PLATFORM_STATIC_META parity with backend registry', () => {
  it('declares the same platform ids as pkg/domain/platform.go', () => {
    const metaKeys = new Set(Object.keys(PLATFORM_STATIC_META));
    for (const id of REGISTRY_IDS_FROM_GO) {
      expect(metaKeys.has(id)).toBe(true);
    }
    expect(metaKeys.size).toBe(REGISTRY_IDS_FROM_GO.length);
  });

  it('mirrors the at-rest status of every platform via defaultStatus', () => {
    for (const [id, expected] of REGISTRY_DEFAULT_STATUS) {
      expect(PLATFORM_STATIC_META[id].defaultStatus).toBe(expected);
    }
  });

  it('exposes a unique displayOrder for every platform', () => {
    const orders = (Object.keys(PLATFORM_STATIC_META) as PlatformId[]).map(
      (id) => PLATFORM_STATIC_META[id].displayOrder
    );
    expect(new Set(orders).size).toBe(orders.length);
  });
});

describe('PLATFORM_DISPLAY_ORDER', () => {
  it('lists every PLATFORM_STATIC_META id once, in displayOrder', () => {
    expect(PLATFORM_DISPLAY_ORDER).toHaveLength(Object.keys(PLATFORM_STATIC_META).length);
    for (let i = 1; i < PLATFORM_DISPLAY_ORDER.length; i++) {
      const prev = PLATFORM_STATIC_META[PLATFORM_DISPLAY_ORDER[i - 1]].displayOrder;
      const next = PLATFORM_STATIC_META[PLATFORM_DISPLAY_ORDER[i]].displayOrder;
      expect(next).toBeGreaterThan(prev);
    }
  });
});

describe('legacy view-objects', () => {
  it('PLATFORM_COLORS / PLATFORM_LABELS are derived from PLATFORM_STATIC_META', () => {
    for (const id of Object.keys(PLATFORM_STATIC_META) as PlatformId[]) {
      expect(PLATFORM_COLORS[id]).toBe(PLATFORM_STATIC_META[id].color);
      expect(PLATFORM_LABELS[id]).toBe(PLATFORM_STATIC_META[id].shortLabel);
    }
  });

  it('createPlatformFullLabels(t) maps every id through the translator', () => {
    const labels = createPlatformFullLabels(tFullLabel);
    for (const id of Object.keys(PLATFORM_STATIC_META) as PlatformId[]) {
      expect(labels[id]).toBe(`label:${id}`);
    }
  });

  it('createPlatformMeta(t) merges static fields with translated copy', () => {
    const meta = createPlatformMeta(tFullLabel, tPlatforms);
    for (const id of Object.keys(PLATFORM_STATIC_META) as PlatformId[]) {
      expect(meta[id].color).toBe(PLATFORM_STATIC_META[id].color);
      expect(meta[id].shortLabel).toBe(PLATFORM_STATIC_META[id].shortLabel);
      expect(meta[id].displayOrder).toBe(PLATFORM_STATIC_META[id].displayOrder);
      expect(meta[id].defaultStatus).toBe(PLATFORM_STATIC_META[id].defaultStatus);
      expect(meta[id].fullLabel).toBe(`label:${id}`);
      if (PLATFORM_STATIC_META[id].defaultStatus === 'coming_soon') {
        expect(meta[id].comingSoonWhen).toBe('coming-soon-when:comingSoonWhen');
      } else {
        expect(meta[id].comingSoonWhen).toBeUndefined();
      }
    }
  });
});

describe('isKnownPlatform', () => {
  it('returns true for registered ids and false for the rest', () => {
    expect(isKnownPlatform('telegram')).toBe(true);
    expect(isKnownPlatform('whatsapp')).toBe(true);
    expect(isKnownPlatform('myspace')).toBe(false);
    expect(isKnownPlatform('')).toBe(false);
  });
});
