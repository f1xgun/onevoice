'use client';

import { useSearchParams } from 'next/navigation';

import { RequirePermission } from '@/components/permission/RequirePermission';
import { RoleEditorForm } from '../_components/RoleEditorForm';

// `/settings/roles/new` — entry point for the create-role flow.
//
// Reads the optional `?clone_from=<roleId>` query param and forwards it to
// `RoleEditorForm` (Plan 05-07, D-04). When `clone_from` resolves a role in
// the cached list, the form pre-fills with «Копия — {sourceName}» + the
// intersection of source permissions and the actor's effective permissions.
//
// Gated by `roles.create` — backend re-checks every POST; this is a UX gate
// only (Phase 5 UI-RBAC-08). The page returns `null` for actors without the
// permission, leaving the SettingsLayout chrome intact so they can navigate
// elsewhere.
export default function NewRolePage() {
  const params = useSearchParams();
  const cloneFromId = params.get('clone_from');
  return (
    <RequirePermission perm="roles.create">
      <RoleEditorForm mode="create" cloneFromId={cloneFromId} />
    </RequirePermission>
  );
}
