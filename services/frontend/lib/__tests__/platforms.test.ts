import { describe, expect, it } from 'vitest';

import {
  PLATFORM_META,
  PLATFORM_DISPLAY_ORDER,
  PLATFORM_COLORS,
  PLATFORM_FULL_LABELS,
  PLATFORM_LABELS,
  isKnownPlatform,
  type PlatformId,
} from '../platforms';

// The Go registry in pkg/domain/platform.go is the authority for which
// platforms exist and their at-rest status. PLATFORM_META is the frontend
// shadow that fills in presentation. These tests pin the parity invariant
// so adding a platform on one side without the other fails fast — rather
// than silently rendering a connect card for a non-MVP platform when the
// React Query fetch races UI render (and therefore returns the static
// fallback first).
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

describe('PLATFORM_META parity with backend registry', () => {
  it('declares the same platform ids as pkg/domain/platform.go', () => {
    const metaKeys = new Set(Object.keys(PLATFORM_META));
    for (const id of REGISTRY_IDS_FROM_GO) {
      expect(metaKeys.has(id)).toBe(true);
    }
    expect(metaKeys.size).toBe(REGISTRY_IDS_FROM_GO.length);
  });

  it('mirrors the at-rest status of every platform via defaultStatus', () => {
    for (const [id, expected] of REGISTRY_DEFAULT_STATUS) {
      expect(PLATFORM_META[id].defaultStatus).toBe(expected);
    }
  });

  it('exposes a unique displayOrder for every platform', () => {
    const orders = (Object.keys(PLATFORM_META) as PlatformId[]).map(
      (id) => PLATFORM_META[id].displayOrder
    );
    expect(new Set(orders).size).toBe(orders.length);
  });
});

describe('PLATFORM_DISPLAY_ORDER', () => {
  it('lists every PLATFORM_META id once, in displayOrder', () => {
    expect(PLATFORM_DISPLAY_ORDER).toHaveLength(Object.keys(PLATFORM_META).length);
    for (let i = 1; i < PLATFORM_DISPLAY_ORDER.length; i++) {
      const prev = PLATFORM_META[PLATFORM_DISPLAY_ORDER[i - 1]].displayOrder;
      const next = PLATFORM_META[PLATFORM_DISPLAY_ORDER[i]].displayOrder;
      expect(next).toBeGreaterThan(prev);
    }
  });
});

describe('legacy view-objects', () => {
  it('PLATFORM_COLORS / LABELS / FULL_LABELS are derived from PLATFORM_META', () => {
    for (const id of Object.keys(PLATFORM_META) as PlatformId[]) {
      expect(PLATFORM_COLORS[id]).toBe(PLATFORM_META[id].color);
      expect(PLATFORM_LABELS[id]).toBe(PLATFORM_META[id].shortLabel);
      expect(PLATFORM_FULL_LABELS[id]).toBe(PLATFORM_META[id].fullLabel);
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
