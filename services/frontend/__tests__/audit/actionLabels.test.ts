import { describe, it, expect } from 'vitest';
import { AUDIT_ACTIONS, type AuditAction } from '@/app/(app)/settings/audit/_lib/types';
import {
  actionToI18nKey,
  actionsForCategory,
  ACTION_LABEL_KEYS,
} from '@/app/(app)/settings/audit/_lib/actionLabels';
import ru from '@/messages/ru.json';

// Drift guard for the audit action surface (B3). AUDIT_ACTIONS in types.ts
// is the single source of truth. This file pins:
//   1. The 21-action count (matches pkg/audit/actions.go).
//   2. Every entry has a corresponding messages/ru.json key.
//   3. ACTION_LABEL_KEYS covers every AUDIT_ACTIONS entry (the
//      Record<AuditAction, string> typing already enforces this at
//      compile time, but a runtime assertion makes the failure
//      message at CI time human-readable rather than a TS error).
//   4. actionsForCategory filters from the canonical list.

const EXPECTED_ACTION_COUNT = 21;

describe('audit action labels drift guard', () => {
  it('AUDIT_ACTIONS has the expected count', () => {
    expect(AUDIT_ACTIONS).toHaveLength(EXPECTED_ACTION_COUNT);
  });

  it('every action has a Russian label in messages/ru.json', () => {
    const labels = (ru as unknown as { audit: { actions: Record<string, string> } }).audit.actions;
    for (const action of AUDIT_ACTIONS) {
      const key = actionToI18nKey(action).replace('audit.actions.', '');
      expect(labels[key], `missing audit.actions.${key} in ru.json`).toBeTypeOf('string');
      expect(labels[key].length, `audit.actions.${key} is empty`).toBeGreaterThan(0);
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
});
