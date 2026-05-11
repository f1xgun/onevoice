'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { MoreHorizontal, Trash2, CopyPlus } from 'lucide-react';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/lib/hooks/usePermission';
import type { Role } from '@/lib/schemas';

import { DeleteRoleDialog } from './DeleteRoleDialog';

// Row-action overflow menu used by both SystemRolesSection and CustomRolesSection.
// Visibility of each item is permission-gated:
//   - «Дублировать» requires usePermission('roles.create') — both system + custom.
//   - «Удалить» requires usePermission('roles.delete') AND !role.is_system —
//     system roles are immutable (backend rejects with system_role_immutable).
//
// NOTE on UI-RBAC-11: the strings `'roles.create'` / `'roles.delete'` passed to
// usePermission are PERMISSION KEYS (dynamic registry from Plan 05-04), not
// hardcoded role→perms maps. UI-RBAC-11 prohibits role→perms maps, not registry
// permission keys at gate sites.

export interface RoleActionsMenuProps {
  role: Role;
  businessId: string | null;
  // Full role list — needed for the DeleteRoleDialog's reassign picker
  // (D-08 picker variant, D-09 ordering). System rows pass [] because they
  // never offer Delete and never need the picker.
  allRoles?: Role[];
}

export function RoleActionsMenu({ role, businessId, allRoles = [] }: RoleActionsMenuProps) {
  const t = useTranslations('roles.list');
  const [deleteOpen, setDeleteOpen] = useState(false);

  const canCreate = usePermission('roles.create').allowed;
  const canDeletePerm = usePermission('roles.delete').allowed;
  const canDelete = canDeletePerm && !role.is_system;

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('menuAria', { name: role.name })}
            // Stop propagation so the parent row-link (custom roles) doesn't
            // navigate when the trigger is clicked.
            onClick={(e) => e.stopPropagation()}
          >
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
          {canCreate && (
            <DropdownMenuItem asChild>
              <Link
                href={`/settings/roles/new?clone_from=${role.id}`}
                className="flex items-center gap-2"
              >
                <CopyPlus className="h-4 w-4" aria-hidden />
                {t('actions.duplicate')}
              </Link>
            </DropdownMenuItem>
          )}
          {canDelete && (
            <DropdownMenuItem
              onSelect={(e) => {
                // Prevent default close-then-open dance — we control the dialog state.
                e.preventDefault();
                setDeleteOpen(true);
              }}
              className="flex items-center gap-2 text-[var(--ov-danger)] focus:text-[var(--ov-danger)]"
            >
              <Trash2 className="h-4 w-4" aria-hidden />
              {t('actions.delete')}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {canDelete && (
        <DeleteRoleDialog
          role={role}
          businessId={businessId}
          allRoles={allRoles}
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
        />
      )}
    </>
  );
}
