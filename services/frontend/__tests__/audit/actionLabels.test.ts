import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { AUDIT_ACTIONS, type AuditAction } from '@/app/(app)/settings/audit/_lib/types';
import {
  actionToI18nKey,
  actionsForCategory,
  ACTION_LABEL_KEYS,
  isKnownAuditAction,
} from '@/app/(app)/settings/audit/_lib/actionLabels';
import ru from '@/messages/ru.json';
import en from '@/messages/en.json';

// Drift guard for the audit action surface (B3). AUDIT_ACTIONS in types.ts
// is the single source of truth for the actions selectable in the journal
// filter dropdown. This file pins:
//   1. The action count (the filterable subset of pkg/audit/actions.go).
//   2. Every entry has a corresponding messages/ru.json and en.json key.
//   3. ACTION_LABEL_KEYS covers every AUDIT_ACTIONS entry (the
//      Record<AuditAction, string> typing already enforces this at
//      compile time, but a runtime assertion makes the failure
//      message at CI time human-readable rather than a TS error).
//   4. actionsForCategory filters from the canonical list.

function localeLabels(bundle: unknown): Record<string, string> {
  return (bundle as { audit: { actions: Record<string, string> } }).audit.actions;
}

function openAPIAuditActions(): string[] {
  const spec = readFileSync(resolve(process.cwd(), '../../docs/api/spec/openapi.yaml'), 'utf8');
  const block = spec.match(
    /    AuditAction:\n      type: string\n      enum:\n((?:      - .+\n)+)/
  );
  if (!block) throw new Error('AuditAction enum not found in OpenAPI spec');
  return block[1]
    .trim()
    .split('\n')
    .map((line) => line.replace(/^\s*-\s*/, ''));
}

describe('audit action labels drift guard', () => {
  it('contains the emitted platform, review autopilot, and HITL actions', () => {
    expect(AUDIT_ACTIONS).toEqual(
      expect.arrayContaining([
        'platform.post_published',
        'platform.dm_sent',
        'platform.review_replied',
        'review.auto_replied',
        'hitl.approval_resolved',
      ])
    );
  });

  it('matches the OpenAPI business-feed action enum exactly', () => {
    expect([...AUDIT_ACTIONS]).toEqual(openAPIAuditActions());
  });

  it('every action has a non-empty label in both ru.json and en.json', () => {
    for (const [locale, bundle] of [
      ['ru', ru],
      ['en', en],
    ] as const) {
      const labels = localeLabels(bundle);
      for (const action of AUDIT_ACTIONS) {
        const key = actionToI18nKey(action).replace('audit.actions.', '');
        expect(labels[key], `missing audit.actions.${key} in ${locale}.json`).toBeTypeOf('string');
        expect(
          labels[key].length,
          `audit.actions.${key} is empty in ${locale}.json`
        ).toBeGreaterThan(0);
      }
    }
  });

  it('ACTION_LABEL_KEYS covers every AUDIT_ACTIONS entry', () => {
    for (const action of AUDIT_ACTIONS) {
      expect(ACTION_LABEL_KEYS[action as AuditAction]).toBe(actionToI18nKey(action));
    }
  });

  it('actionsForCategory filters from AUDIT_ACTIONS by prefix', () => {
    expect(actionsForCategory('all')).toEqual(AUDIT_ACTIONS);
    const rbacOnly = actionsForCategory('rbac');
    expect(rbacOnly.length).toBeGreaterThan(0);
    for (const a of rbacOnly) {
      expect(a.startsWith('rbac.')).toBe(true);
    }
    const authOnly = actionsForCategory('auth');
    for (const a of authOnly) {
      expect(a.startsWith('auth.')).toBe(true);
    }
  });

  it('recognizes catalog actions and preserves an unknown-action fallback', () => {
    expect(isKnownAuditAction('review.auto_replied')).toBe(true);
    expect(isKnownAuditAction('future.action')).toBe(false);
  });
});
