'use client';

import { ChevronDown } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Checkbox } from '@/components/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import type { PermissionGroup } from '@/lib/schemas';

import { LeafCheckbox } from './LeafCheckbox';

export type GroupTriState = 'checked' | 'unchecked' | 'indeterminate';

export interface GroupRowProps {
  group: PermissionGroup;
  /** Currently-selected permission keys (mirror of `valueSet`, kept for splice). */
  value: string[];
  /** Set form of `value` — passed in to avoid rebuilding the Set per group. */
  valueSet: Set<string>;
  /** Set of permissions the actor holds. Drives tri-state + per-leaf disable. */
  actorPermissions: Set<string>;
  /** Form-level disable (e.g. during submit). */
  disabled: boolean;
  onChange: (next: string[]) => void;
}

/**
 * Pure-function tri-state derivation ( invariant).
 *
 * Returns 'checked' only when every actor-enabled leaf is selected. Disabled
 * leaves are NEVER part of the count — they preserve whatever the role
 * already had, so a partial-actor toggling the group never appears stuck on
 * indeterminate just because they can't touch the disabled leaves.
 *
 * Edge case: actor has zero enabled leaves in this group → 'unchecked' (the
 * checkbox is still rendered but `handleGroupToggle` is a no-op).
 */
export function computeGroupState(
  group: PermissionGroup,
  valueSet: Set<string>,
  actorPerms: Set<string>
): GroupTriState {
  const enabledLeaves = group.permissions.filter((p) => actorPerms.has(p.name));
  if (enabledLeaves.length === 0) return 'unchecked';
  const selectedEnabled = enabledLeaves.filter((p) => valueSet.has(p.name));
  if (selectedEnabled.length === 0) return 'unchecked';
  if (selectedEnabled.length === enabledLeaves.length) return 'checked';
  return 'indeterminate';
}

/**
 * Pure-function group-toggle handler ( invariant).
 *
 * Flips ONLY the actor-enabled leaves; disabled leaves stay in their current
 * state. 'unchecked' / 'indeterminate' both flip to 'all enabled selected';
 * 'checked' flips to 'no enabled selected'.
 */
export function handleGroupToggle(
  group: PermissionGroup,
  currentState: GroupTriState,
  value: string[],
  actorPerms: Set<string>,
  onChange: (next: string[]) => void
): void {
  const enabledKeys = group.permissions.filter((p) => actorPerms.has(p.name)).map((p) => p.name);
  if (enabledKeys.length === 0) return;
  const next = new Set(value);
  if (currentState === 'checked') {
    enabledKeys.forEach((k) => next.delete(k));
  } else {
    enabledKeys.forEach((k) => next.add(k));
  }
  onChange(Array.from(next));
}

/**
 * Group header row: tri-state checkbox + chevron + resource label + N/M count.
 * Wraps a `<Collapsible defaultOpen>` so every group is expanded on
 * first paint — RBAC discoverability matters more than vertical density for
 * the v2.0 catalog of 21 permissions.
 */
export function GroupRow({
  group,
  value,
  valueSet,
  actorPermissions,
  disabled,
  onChange,
}: GroupRowProps) {
  const t = useTranslations('roles.permissionTree');
  const state = computeGroupState(group, valueSet, actorPermissions);
  const selectedCount = group.permissions.filter((p) => valueSet.has(p.name)).length;

  return (
    <Collapsible defaultOpen className="group/trigger" data-resource={group.resource}>
      <div className="bg-paper-raised/40 flex items-center gap-2 rounded-md border border-line-soft px-2 py-2">
        <Checkbox
          checked={state === 'indeterminate' ? 'indeterminate' : state === 'checked'}
          disabled={disabled}
          aria-label={group.resource}
          onCheckedChange={() => handleGroupToggle(group, state, value, actorPermissions, onChange)}
        />
        <CollapsibleTrigger className="flex flex-1 items-center gap-2 text-left text-ink hover:text-ink-mid">
          <ChevronDown className="h-4 w-4 transition-transform duration-150 group-data-[state=closed]/trigger:-rotate-90" />
          <span className="font-medium capitalize">{group.resource}</span>
          <span className="ml-auto font-mono text-xs text-ink-soft" aria-hidden>
            {t('groupCount', { selected: selectedCount, total: group.permissions.length })}
          </span>
        </CollapsibleTrigger>
      </div>
      <CollapsibleContent>
        <ul className="ml-6 mt-1 space-y-0" role="list">
          {group.permissions.map((leaf) => {
            const leafChecked = valueSet.has(leaf.name);
            const actorHas = actorPermissions.has(leaf.name);
            return (
              <LeafCheckbox
                key={leaf.name}
                leafName={leaf.name}
                description={leaf.description}
                checked={leafChecked}
                disabled={disabled || !actorHas}
                actorHas={actorHas}
                onToggle={(checked) => {
                  const next = new Set(value);
                  if (checked) next.add(leaf.name);
                  else next.delete(leaf.name);
                  onChange(Array.from(next));
                }}
              />
            );
          })}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  );
}
