'use client';

import { useSearchParams } from 'next/navigation';

import { RequirePermission } from '@/components/permission/RequirePermission';
import { RoleEditorForm } from '../_components/RoleEditorForm';

// Gated by `roles.create` — backend re-checks every POST; this is a UX gate
// only. The page returns `null` for actors without the permission, leaving
// the SettingsLayout chrome intact so they can navigate elsewhere.
export default function NewRolePage() {
  const params = useSearchParams();
  const cloneFromId = params.get('clone_from');
  return (
    <RequirePermission perm="roles.create">
      <RoleEditorForm mode="create" cloneFromId={cloneFromId} />
    </RequirePermission>
  );
}
