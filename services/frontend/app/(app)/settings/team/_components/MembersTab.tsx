'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { MoreHorizontal } from 'lucide-react';
import { format, parseISO } from 'date-fns';
import { ru } from 'date-fns/locale';
import { toast } from 'sonner';

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Skeleton } from '@/components/ui/skeleton';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { useMembers, useRemoveMember, useUpdateMemberRole } from '@/lib/hooks/useMembers';
import { useMapMemberError } from '@/lib/resolveErrorMap';
import { usePermission } from '@/lib/hooks/usePermission';
import type { Member, Role } from '@/lib/schemas';
import { RolePill } from '@/components/business-switcher/RolePill';
import { useAuthStore } from '@/lib/auth';

import { ConfirmDestructive } from './ConfirmDestructive';
import { RoleChangeDialog } from './RoleChangeDialog';

const SKELETON_ROW_COUNT = 4;

interface MembersTabProps {
  businessId: string;
  roles: Role[];
}

export function MembersTab({ businessId, roles }: MembersTabProps) {
  const tTeam = useTranslations('team');
  const tCols = useTranslations('team.members.cols');
  const tActions = useTranslations('team.members.actions');
  const mapMemberError = useMapMemberError();
  const { data: members, isLoading, isError } = useMembers(businessId);
  const currentUserId = useAuthStore((s) => s.user?.id);
  const updateRole = useUpdateMemberRole(businessId);
  const removeMember = useRemoveMember(businessId);

  const canUpdateRole = usePermission('members.update_role').allowed;
  const canRemove = usePermission('members.remove').allowed;

  const [roleChange, setRoleChange] = useState<Member | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<Member | null>(null);

  // Defensive: gate against the auth-store hydration race where currentUserId
  // is still undefined on first paint (HI-01 in 04-REVIEW.md).
  const isSelf = (m: Member) => currentUserId !== undefined && m.user.id === currentUserId;

  const handleRoleChange = async (m: Member, newRoleId: string) => {
    try {
      await updateRole.mutateAsync({ userId: m.user.id, roleId: newRoleId });
      toast.success(tTeam('members.changeRole.toastSuccess'));
    } catch (err) {
      toast.error(mapMemberError(err));
      throw err;
    }
  };

  const handleRemove = async (m: Member) => {
    try {
      await removeMember.mutateAsync(m.user.id);
      toast.success(tTeam('members.remove.toastSuccess'));
    } catch (err) {
      toast.error(mapMemberError(err));
    } finally {
      setConfirmRemove(null);
    }
  };

  if (isLoading) {
    return (
      <div className="rounded-lg border border-line bg-paper-raised p-6">
        {Array.from({ length: SKELETON_ROW_COUNT }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 py-3">
            <Skeleton className="h-6 w-6 rounded-full" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="ml-auto h-4 w-24" />
          </div>
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="rounded-lg border border-line bg-paper-raised p-6">
        <p className="text-sm text-danger">{tTeam('members.loadError')}</p>
      </div>
    );
  }

  return (
    <TooltipProvider>
      <section className="rounded-lg border border-line bg-paper-raised">
        <header className="border-b border-line-soft px-6 py-4">
          <p className="text-[11px] font-medium uppercase tracking-[0.04em] text-ink-soft">
            {tTeam('members.kicker')}
          </p>
          <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">
            {tTeam('members.count', { count: members?.length ?? 0 })}
          </h2>
        </header>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{tCols('name')}</TableHead>
              <TableHead>{tCols('email')}</TableHead>
              <TableHead>{tCols('role')}</TableHead>
              <TableHead>{tCols('joined')}</TableHead>
              <TableHead className="text-right">{tCols('actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(members ?? []).map((m) => {
              const showSelf = isSelf(m);
              const canActOnThisRow = canUpdateRole || canRemove || showSelf;
              return (
                <TableRow key={m.user.id}>
                  <TableCell className="text-sm text-ink">
                    {m.user.name ?? m.user.email.split('@')[0]}
                  </TableCell>
                  <TableCell className="font-mono text-sm text-ink-mid">{m.user.email}</TableCell>
                  <TableCell>
                    <RolePill roleName={m.role.name} />
                  </TableCell>
                  <TableCell className="text-xs text-ink-soft">
                    {format(parseISO(m.joined_at), 'd MMMM yyyy', { locale: ru })}
                  </TableCell>
                  <TableCell className="text-right">
                    {!canActOnThisRow ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span tabIndex={-1} aria-disabled="true">
                            <Button variant="ghost" size="icon" disabled className="h-8 w-8">
                              <MoreHorizontal size={16} aria-hidden />
                            </Button>
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>{tActions('disabledHint')}</TooltipContent>
                      </Tooltip>
                    ) : (
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8"
                            aria-label={tActions('menuAria', { name: m.user.email })}
                          >
                            <MoreHorizontal size={16} aria-hidden />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          {canUpdateRole && !showSelf && (
                            <DropdownMenuItem onSelect={() => setRoleChange(m)}>
                              {tActions('changeRole')}
                            </DropdownMenuItem>
                          )}
                          {(showSelf || canRemove) && (
                            <DropdownMenuItem
                              className="text-danger focus:text-danger"
                              onSelect={(e) => {
                                e.preventDefault();
                                setConfirmRemove(m);
                              }}
                            >
                              {showSelf ? tActions('leave') : tActions('remove')}
                            </DropdownMenuItem>
                          )}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </section>

      {roleChange && (
        <RoleChangeDialog
          // Key by member id so React re-mounts the dialog per member;
          // useState(currentRoleId) initializer otherwise sticks to the
          // previously-opened member's value (ME-02 in 04-REVIEW.md).
          key={roleChange.user.id}
          open
          onOpenChange={() => setRoleChange(null)}
          memberName={roleChange.user.name ?? roleChange.user.email}
          currentRoleId={roleChange.role.id}
          roles={roles}
          onSubmit={(newRoleId) => handleRoleChange(roleChange, newRoleId)}
        />
      )}

      {confirmRemove && (
        <ConfirmDestructive
          open
          onOpenChange={() => setConfirmRemove(null)}
          title={
            isSelf(confirmRemove) ? tTeam('members.leave.title') : tTeam('members.remove.title')
          }
          body={
            isSelf(confirmRemove)
              ? tTeam('members.leave.body', { businessName: '' })
              : tTeam('members.remove.body', {
                  name: confirmRemove.user.name ?? confirmRemove.user.email,
                })
          }
          confirmLabel={
            isSelf(confirmRemove) ? tTeam('members.leave.confirm') : tTeam('members.remove.confirm')
          }
          onConfirm={() => handleRemove(confirmRemove)}
        />
      )}
    </TooltipProvider>
  );
}
