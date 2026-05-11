'use client';

import { RequirePermission } from '@/components/permission/RequirePermission';
import { RoleEditorForm } from '../../_components/RoleEditorForm';

interface EditRolePageProps {
  params: { id: string };
}

// `/settings/roles/[id]/edit` — entry point for the update-role flow.
//
// Reads `params.id` from the URL and forwards it to `RoleEditorForm` in edit
// mode. The form hydrates name + description + permissions from the cached
// roles list (`useRoles`) — no extra fetch if the user navigated here from
// the list page. Cold-load fires the cache fetch and resets the form on
// hydration (Plan 05-07).
//
// Gated by `roles.update` — backend re-checks every PATCH; this is a UX gate
// only (Phase 5 UI-RBAC-08). The page returns `null` for actors without the
// permission so they don't see a stale form they cannot submit.
export default function EditRolePage({ params }: EditRolePageProps) {
  return (
    <RequirePermission perm="roles.update">
      <RoleEditorForm mode="edit" roleId={params.id} />
    </RequirePermission>
  );
}
