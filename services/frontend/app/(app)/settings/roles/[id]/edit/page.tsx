'use client';

import { useEffect, useState } from 'react';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { RoleEditorForm } from '../../_components/RoleEditorForm';

// `/settings/roles/[id]/edit` — entry point for the update-role flow.
//
// Reads `params.id` from the URL and forwards it to `RoleEditorForm` in edit
// mode. The form hydrates name + description + permissions from the cached
// roles list (`useRoles`) — no extra fetch if the user navigated here from
// the list page. Cold-load fires the cache fetch and resets the form on
// hydration (Plan 05-07).
//
// Next.js 15 makes `params` a Promise. We unwrap it via `useEffect`+`useState`
// rather than `React.use(params)` so the page renders correctly in vitest's
// React 18.3 environment, which does not expose `use()`. Render an empty
// placeholder until the id is resolved — the form would gate on it anyway.
//
// Gated by `roles.update` — backend re-checks every PATCH; this is a UX gate
// only (Phase 5 UI-RBAC-08). The page returns `null` for actors without the
// permission so they don't see a stale form they cannot submit.
export default function EditRolePage({ params }: { params: Promise<{ id: string }> }) {
  const [id, setId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void params.then((resolved) => {
      if (!cancelled) setId(resolved.id);
    });
    return () => {
      cancelled = true;
    };
  }, [params]);

  if (!id) return null;
  return (
    <RequirePermission perm="roles.update">
      <RoleEditorForm mode="edit" roleId={id} />
    </RequirePermission>
  );
}
