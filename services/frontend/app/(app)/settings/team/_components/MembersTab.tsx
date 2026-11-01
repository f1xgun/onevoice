'use client';

import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';

import { useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { MoreHorizontal } from 'lucide-react';
import { format, parseISO } from 'date-fns';
import { toast } from 'sonner';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
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
import { useRoleLabel } from '@/lib/hooks/useRoleLabel';
import { useAuthStore } from '@/lib/auth';

import { ConfirmDestructive } from '@/components/design-system/ConfirmDestructive';
import { RoleChangeDialog } from './RoleChangeDialog';

const SKELETON_ROW_COUNT = 4;

interface MembersTabProps {
  businessId: string;
  roles: Role[];
}

export function MembersTab({ businessId, roles }: MembersTabProps) {
  const tTeam = useTranslations('team');
  const tV2 = useTranslations('settings.v2');
  const getRoleLabel = useRoleLabel();
  const tActions = useTranslations('team.members.actions');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const mapMemberError = useMapMemberError();
  const { data: members, isLoading, isError } = useMembers(businessId);
  const currentUserId = useAuthStore((s) => s.user?.id);
  const updateRole = useUpdateMemberRole(businessId);
  const removeMember = useRemoveMember(businessId);

  const canUpdateRole = usePermission('members.update_role').allowed;
  const canRemove = usePermission('members.remove').allowed;

  const [roleChange, setRoleChange] = useState<Member | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<Member | null>(null);

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
      <LoadingPlaceholder className="rounded-lg border border-line bg-paper-raised p-6">
        {Array.from({ length: SKELETON_ROW_COUNT }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 py-3">
            <Skeleton className="h-6 w-6 rounded-full" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="ml-auto h-4 w-24" />
          </div>
        ))}
      </LoadingPlaceholder>
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
      <ul className="rounded-block bg-block p-1 md:p-2">
        {(members ?? []).map((m) => {
          const showSelf = isSelf(m);
          const canActOnThisRow = canUpdateRole || canRemove || showSelf;
          return (
            <li key={m.user.id} className="flex items-start gap-3 p-3">
              <span
                aria-hidden
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-avatar text-meta font-semibold text-avatar-ink"
              >
                {(m.user.name ?? m.user.email)
                  .trim()
                  .split(/\s+/)
                  .slice(0, 2)
                  .map((part) => part[0])
                  .join('')
                  .toUpperCase()}
              </span>
              <div className="min-w-0 flex-1 break-words">
                <p className="font-semibold">
                  {m.user.name ?? m.user.email.split('@')[0]}
                  {showSelf && ` (${tV2('you')})`}
                </p>
                <p className="text-reading">{getRoleLabel(m.role.name)}</p>
                {roles.find((role) => role.id === m.role.id)?.description && (
                  <p className="text-[15px] leading-[22px]">
                    {roles.find((role) => role.id === m.role.id)?.description}
                  </p>
                )}
                <p className="text-meta text-ink-soft">{m.user.email}</p>
                <span className="sr-only">
                  {format(parseISO(m.joined_at), 'd MMMM yyyy', { locale: dateFnsLocale })}
                </span>
              </div>
              <div>
                {!canActOnThisRow ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span tabIndex={-1} aria-disabled="true">
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled
                          aria-label={tActions('menuAria', { name: m.user.email })}
                          className="h-8 w-8"
                        >
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
                    <DropdownMenuContent
                      className="max-w-[calc(100vw-2rem)] border-control bg-card text-ink shadow-overlay"
                      align="end"
                    >
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
              </div>
            </li>
          );
        })}
      </ul>

      {roleChange && (
        <RoleChangeDialog
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
