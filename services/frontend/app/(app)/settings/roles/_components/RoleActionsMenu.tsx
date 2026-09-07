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
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { usePermission } from '@/lib/hooks/usePermission';
import type { Role } from '@/lib/schemas';

import { DeleteRoleDialog } from './DeleteRoleDialog';

// «Удалить» requires !role.is_system — system roles are immutable
// (backend rejects with system_role_immutable).

export interface RoleActionsMenuProps {
  role: Role;
  businessId: string | null;
  // Full role list — needed for the DeleteRoleDialog's reassign picker.
  // System rows pass [] because they never offer Delete.
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
            onClick={(e) => e.stopPropagation()}
          >
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          className="max-w-[calc(100vw-2rem)] border-control bg-card text-ink shadow-overlay"
          align="end"
          onClick={(e) => e.stopPropagation()}
        >
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
