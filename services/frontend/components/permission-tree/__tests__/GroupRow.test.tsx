import { describe, expect, it } from 'vitest';

import type { PermissionGroup } from '@/lib/schemas';

import { computeGroupState, handleGroupToggle } from '../GroupRow';

// Plan 05-05 Task 3 — pure-function unit tests for the locked D-12 invariant.
//
// computeGroupState + handleGroupToggle are extracted as exports so the
// tri-state contract is auditable without DOM/RTL plumbing. The integration
// tests in PermissionTree.test.tsx + LeafCheckbox.test.tsx prove the wiring;
// these prove the algorithm.

const GROUP: PermissionGroup = {
  resource: 'roles',
  permissions: [
    { name: 'roles.read', description: '' },
    { name: 'roles.create', description: '' },
    { name: 'roles.update', description: '' },
    { name: 'roles.delete', description: '' },
  ],
};

const ACTOR_FULL = new Set(['roles.read', 'roles.create', 'roles.update', 'roles.delete']);
const ACTOR_PARTIAL = new Set(['roles.read', 'roles.update']); // missing create + delete
const ACTOR_EMPTY = new Set<string>();

describe('computeGroupState (D-12 invariant)', () => {
  it('returns "unchecked" when no leaves selected', () => {
    expect(computeGroupState(GROUP, new Set(), ACTOR_FULL)).toBe('unchecked');
  });

  it('returns "checked" when every actor-enabled leaf selected', () => {
    expect(
      computeGroupState(
        GROUP,
        new Set(['roles.read', 'roles.create', 'roles.update', 'roles.delete']),
        ACTOR_FULL
      )
    ).toBe('checked');
  });

  it('returns "indeterminate" when some (not all) actor-enabled leaves selected', () => {
    expect(computeGroupState(GROUP, new Set(['roles.read']), ACTOR_FULL)).toBe('indeterminate');
  });

  it('partial actor: "checked" requires only enabled leaves selected', () => {
    // Actor can only grant read+update. When both are selected the group is
    // "checked" even though create+delete are disabled and unselected —
    // because the disabled leaves never count toward the tri-state.
    expect(computeGroupState(GROUP, new Set(['roles.read', 'roles.update']), ACTOR_PARTIAL)).toBe(
      'checked'
    );
  });

  it('partial actor: "indeterminate" when only some enabled leaves selected', () => {
    expect(computeGroupState(GROUP, new Set(['roles.read']), ACTOR_PARTIAL)).toBe('indeterminate');
  });

  it('partial actor ignores disabled leaves already in value when deriving state', () => {
    // roles.delete is in value but actor can't toggle it — should not push
    // the tri-state to "indeterminate" because the enabled set is empty.
    expect(computeGroupState(GROUP, new Set(['roles.delete']), ACTOR_PARTIAL)).toBe('unchecked');
  });

  it('actor with zero enabled leaves in group → "unchecked"', () => {
    expect(computeGroupState(GROUP, new Set(), ACTOR_EMPTY)).toBe('unchecked');
    // Even if value contains every leaf, with zero enabled the group is
    // "unchecked" (the user can't manipulate anything anyway).
    expect(
      computeGroupState(
        GROUP,
        new Set(['roles.read', 'roles.create', 'roles.update', 'roles.delete']),
        ACTOR_EMPTY
      )
    ).toBe('unchecked');
  });
});

describe('handleGroupToggle (D-12: skips disabled leaves)', () => {
  it('toggles ON: adds every actor-enabled leaf; disabled leaves stay', () => {
    let result: string[] = [];
    handleGroupToggle(GROUP, 'unchecked', ['roles.delete'], ACTOR_PARTIAL, (n) => {
      result = n;
    });
    // Actor toggles read + update on; delete (disabled) was already in value
    // → stays in value.
    expect([...result].sort()).toEqual(['roles.delete', 'roles.read', 'roles.update'].sort());
  });

  it('toggles OFF: removes every actor-enabled leaf; disabled leaves stay', () => {
    let result: string[] = [];
    handleGroupToggle(
      GROUP,
      'checked',
      ['roles.read', 'roles.update', 'roles.delete'],
      ACTOR_PARTIAL,
      (n) => {
        result = n;
      }
    );
    // Toggle off read + update; delete (disabled) preserved.
    expect([...result].sort()).toEqual(['roles.delete']);
  });

  it('indeterminate → checked: fills in every enabled leaf', () => {
    let result: string[] = [];
    handleGroupToggle(GROUP, 'indeterminate', ['roles.read'], ACTOR_PARTIAL, (n) => {
      result = n;
    });
    expect([...result].sort()).toEqual(['roles.read', 'roles.update'].sort());
  });

  it('actor with zero enabled leaves: toggle is a no-op (onChange not called)', () => {
    let called = false;
    handleGroupToggle(GROUP, 'unchecked', ['roles.read'], ACTOR_EMPTY, () => {
      called = true;
    });
    expect(called).toBe(false);
  });

  it('full actor toggle ON from unchecked: adds every leaf', () => {
    let result: string[] = [];
    handleGroupToggle(GROUP, 'unchecked', [], ACTOR_FULL, (n) => {
      result = n;
    });
    expect([...result].sort()).toEqual(
      ['roles.create', 'roles.delete', 'roles.read', 'roles.update'].sort()
    );
  });

  it('full actor toggle OFF from checked: clears every leaf', () => {
    let result: string[] = [];
    handleGroupToggle(
      GROUP,
      'checked',
      ['roles.read', 'roles.create', 'roles.update', 'roles.delete'],
      ACTOR_FULL,
      (n) => {
        result = n;
      }
    );
    expect(result).toEqual([]);
  });

  it('preserves unrelated permissions in the value array', () => {
    // Permissions from other groups must not be touched by the toggle.
    let result: string[] = [];
    handleGroupToggle(GROUP, 'unchecked', ['business.read'], ACTOR_PARTIAL, (n) => {
      result = n;
    });
    expect([...result].sort()).toEqual(['business.read', 'roles.read', 'roles.update'].sort());
  });
});
