'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import type { AxiosError } from 'axios';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/design-system/AppAlertDialog';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useDeleteRole } from '@/lib/hooks/useRoles';
import { getMyPermissions } from '@/lib/api/permissions';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { SYSTEM_ROLE_ORDER } from '@/lib/constants/roles';
import { useMapRoleError } from '@/lib/resolveErrorMap';
import { useRoleLabel } from '@/lib/hooks/useRoleLabel';
import type { Role } from '@/lib/schemas';

// DeleteRoleDialog — the / / nucleus of the roles list flow.
//
// -  smart variant: the dialog opens in 'simple' (plain confirm) when
// the cached role.member_count is 0, and in 'picker' (Select required)
// when it's >0. This lets the most-common case (delete an unused role)
// resolve in one click.
// -  reassign ordering: when in picker mode, the Select shows system
// roles first in fixed Owner→Admin→Editor→Viewer order, then custom
// roles A→Z. Targets the actor can't grant (escalation guard) render
// disabled.
// -  race recovery: when the dialog opened in 'simple' but the
// backend rejects the DELETE with 422 role_in_use (someone else
// assigned a member between cache read and click), the dialog
// SILENTLY invalidates+refetches the roles cache and flips itself to
// 'picker' WITHOUT closing. Radix preserves focus; users see the body
// swap in-place rather than a close-then-reopen flash.

interface BuildReassignOptionsArg {
  allRoles: Role[];
  excludeRoleId: string;
  actorPerms: Set<string>;
}

export interface ReassignOption {
  value: string;
  label: string;
  disabled: boolean;
  isSystem: boolean;
  // Raw role name — kept so consumers can localize the label themselves
  // if they prefer. The default `label` is the Russian localized form
  // for system roles, raw name for custom roles.
  rawName: string;
}

// Pure helper — testable without React. Builds the ordered Select option
// list. Exported so the test suite can exercise edge cases
// (escalation guard, self-exclusion) directly against the function.
export function buildReassignOptions({
  allRoles,
  excludeRoleId,
  actorPerms,
}: BuildReassignOptionsArg): ReassignOption[] {
  const filtered = allRoles.filter((r) => r.id !== excludeRoleId);

  const system = filtered
    .filter((r) => r.is_system)
    .sort((a, b) => {
      const ai = SYSTEM_ROLE_ORDER.indexOf(a.name as (typeof SYSTEM_ROLE_ORDER)[number]);
      const bi = SYSTEM_ROLE_ORDER.indexOf(b.name as (typeof SYSTEM_ROLE_ORDER)[number]);
      return (
        (ai === -1 ? SYSTEM_ROLE_ORDER.length : ai) - (bi === -1 ? SYSTEM_ROLE_ORDER.length : bi)
      );
    });
  const custom = filtered
    .filter((r) => !r.is_system)
    .sort((a, b) => a.name.localeCompare(b.name, 'ru'));

  const SYSTEM_LABEL_RU: Record<string, string> = {
    owner: 'Владелец',
    admin: 'Администратор',
    editor: 'Редактор',
    viewer: 'Наблюдатель',
  };

  const canGrant = (role: Role) => role.permissions.every((p) => actorPerms.has(p));

  return [
    ...system.map((r) => ({
      value: r.id,
      label: SYSTEM_LABEL_RU[r.name] ?? r.name,
      disabled: !canGrant(r),
      isSystem: true,
      rawName: r.name,
    })),
    ...custom.map((r) => ({
      value: r.id,
      label: r.name,
      disabled: !canGrant(r),
      isSystem: false,
      rawName: r.name,
    })),
  ];
}

export interface DeleteRoleDialogProps {
  role: Role;
  businessId: string | null;
  allRoles: Role[];
  open: boolean;
  onOpenChange: (next: boolean) => void;
}

export function DeleteRoleDialog({
  role,
  businessId,
  allRoles,
  open,
  onOpenChange,
}: DeleteRoleDialogProps) {
  const t = useTranslations('roles.delete');
  const tList = useTranslations('roles.list');
  const getRoleLabel = useRoleLabel();
  const mapRoleError = useMapRoleError();
  const qc = useQueryClient();
  const deleteMut = useDeleteRole(businessId);

  const initialVariant: 'simple' | 'picker' = (role.member_count ?? 0) > 0 ? 'picker' : 'simple';
  const [variant, setVariant] = useState<'simple' | 'picker'>(initialVariant);
  const [reassignToId, setReassignToId] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setVariant((role.member_count ?? 0) > 0 ? 'picker' : 'simple');
      setReassignToId(null);
    }
  }, [open, role.member_count]);

  const { data: actorPermsArray } = useQuery({
    queryKey: QUERY_KEYS.PERMISSIONS(businessId),
    queryFn: () => getMyPermissions(businessId as string),
    enabled: !!businessId && open,
  });
  const actorPerms = useMemo(() => new Set(actorPermsArray ?? []), [actorPermsArray]);

  const options = useMemo(
    () => buildReassignOptions({ allRoles, excludeRoleId: role.id, actorPerms }),
    [allRoles, role.id, actorPerms]
  );
  const systemOptions = options.filter((o) => o.isSystem);
  const customOptions = options.filter((o) => !o.isSystem);
  const firstEligible = options.find((o) => !o.disabled);

  useEffect(() => {
    if (variant === 'picker' && !reassignToId && firstEligible) {
      setReassignToId(firstEligible.value);
    }
  }, [variant, reassignToId, firstEligible]);

  async function handleConfirm() {
    try {
      if (variant === 'simple') {
        await deleteMut.mutateAsync({ roleId: role.id, reassignTo: null });
      } else {
        if (!reassignToId) return;
        await deleteMut.mutateAsync({ roleId: role.id, reassignTo: reassignToId });
      }
      toast.success(t('toastSuccess'));
      onOpenChange(false);
    } catch (err) {
      const axiosErr = err as AxiosError<{ error?: string }> | undefined;
      const isRoleInUse =
        axiosErr?.response?.status === HTTP_STATUS.UNPROCESSABLE_ENTITY &&
        axiosErr?.response?.data?.error === 'role_in_use';

      if (isRoleInUse && variant === 'simple') {
        await qc.invalidateQueries({ queryKey: QUERY_KEYS.ROLES(businessId) });
        await qc.refetchQueries({ queryKey: QUERY_KEYS.ROLES(businessId) });
        setVariant('picker');
        return;
      }
      toast.error(mapRoleError(err));
    }
  }

  const handleOpenChange = (next: boolean) => {
    if (deleteMut.isPending && !next) return;
    onOpenChange(next);
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent className="max-w-md rounded-lg border border-line bg-paper-raised p-6 shadow-ov-2">
        <AlertDialogHeader className="gap-2">
          <AlertDialogTitle className="text-lg font-medium tracking-tight text-ink">
            {t('title', { name: getRoleLabel(role.name) })}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-sm leading-relaxed text-ink-mid">
            {variant === 'simple'
              ? t('simpleBody')
              : t('pickerBody', { count: role.member_count ?? 0 })}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {variant === 'picker' && (
          <div className="mt-2 space-y-2">
            <label htmlFor="reassign-target" className="text-sm font-medium text-ink">
              {t('pickerLabel')}
            </label>
            <Select value={reassignToId ?? undefined} onValueChange={setReassignToId}>
              <SelectTrigger id="reassign-target" aria-label={t('pickerLabel')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {systemOptions.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>{tList('systemSection')}</SelectLabel>
                    {systemOptions.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value} disabled={opt.disabled}>
                        {getRoleLabel(opt.rawName)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                )}
                {systemOptions.length > 0 && customOptions.length > 0 && <SelectSeparator />}
                {customOptions.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>{tList('customSection')}</SelectLabel>
                    {customOptions.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value} disabled={opt.disabled}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                )}
              </SelectContent>
            </Select>
          </div>
        )}

        <AlertDialogFooter className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <AlertDialogCancel disabled={deleteMut.isPending}>{t('cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="danger"
            onClick={(e) => {
              e.preventDefault();
              void handleConfirm();
            }}
            disabled={deleteMut.isPending || (variant === 'picker' && !reassignToId)}
          >
            {t('confirm')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
