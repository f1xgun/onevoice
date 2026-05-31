'use client';

import { useEffect, useState } from 'react';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { RoleEditorForm } from '../../_components/RoleEditorForm';

// Next.js 15 makes `params` a Promise. We unwrap it via `useEffect`+`useState`
// rather than `React.use(params)` so the page renders correctly in vitest's
// React 18.3 environment, which does not expose `use()`.
//
// Gated by `roles.update` — backend re-checks every PATCH; this is a UX gate
// only. The page returns `null` for actors without the permission so they
// don't see a stale form they cannot submit.
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
