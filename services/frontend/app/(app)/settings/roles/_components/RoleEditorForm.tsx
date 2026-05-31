'use client';

import { useEffect, useMemo } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { ChevronLeft } from 'lucide-react';
import { z } from 'zod';

import { useBusinessStore } from '@/lib/stores/business';
import { useRoles, useCreateRole, useUpdateRole } from '@/lib/hooks/useRoles';
import { getMyPermissions } from '@/lib/api/permissions';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useUnsavedChangesPrompt } from '@/lib/hooks/useUnsavedChangesPrompt';
import { useMapRoleError } from '@/lib/resolveErrorMap';
import { PermissionTree } from '@/components/permission-tree/PermissionTree';
import { TooltipProvider } from '@/components/ui/tooltip';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';

// Schema lives at module scope (not inside the component) so RHF's
// `resolver` keeps stable identity across renders. That requires the
// validation messages to be plain codes ('name_required') that the
// `nameErrorMessage` mapper below translates at render time — the schema
// itself has no React hook context. Add a new code → add a branch below.
const roleEditorSchema = z.object({
  name: z.string().trim().min(1, { message: 'name_required' }),
  description: z.string(),
  permissions: z.array(z.string()),
});

type RoleEditorFormValues = z.infer<typeof roleEditorSchema>;

export interface RoleEditorFormProps {
  mode: 'create' | 'edit';
  /** Required when mode === 'edit'. Identifies the role to load + patch. */
  roleId?: string;
  /** Optional when mode === 'create'. Source role for clone pre-fill. */
  cloneFromId?: string | null;
}

/**
 * Shared role editor form for `/settings/roles/new` and
 * `/settings/roles/[id]/edit`.
 *
 * Clone pre-fill uses `sourcePerms ∩ actorPerms` as a UX affordance — the
 * backend re-validates via CheckEscalationSubset and returns 403 if the
 * client attempts to bypass.
 *
 * PermissionTree leaves render a Tooltip — wrap the whole form in
 * TooltipProvider so the Radix portals find their context.
 */
export function RoleEditorForm({ mode, roleId, cloneFromId }: RoleEditorFormProps) {
  const t = useTranslations('roles.editor');
  const mapRoleError = useMapRoleError();
  const router = useRouter();
  const businessId = useBusinessStore((s) => s.activeBusinessId);

  // Roles list is cached by the list page; if the user lands here cold the
  // hook fires its own fetch. Both create (clone source) and edit modes
  // read the source role from the same cache — no double-fetch.
  const { data: rolesList } = useRoles(businessId);
  const sourceId = mode === 'edit' ? roleId : (cloneFromId ?? undefined);
  const sourceRole = sourceId ? rolesList?.find((r) => r.id === sourceId) : undefined;

  // Actor's effective permissions in the active business. Same query key as
  // usePermission so the cache is shared.
  const { data: actorPermsArray } = useQuery({
    queryKey: QUERY_KEYS.PERMISSIONS(businessId),
    queryFn: () => getMyPermissions(businessId as string),
    enabled: !!businessId,
  });
  const actorPermsSet = useMemo<Set<string>>(
    () => new Set(actorPermsArray ?? []),
    [actorPermsArray]
  );

  // Compute defaultValues based on mode + source. Re-computes when the source
  // role hydrates after a cache miss; the effect below resets the form so RHF
  // picks up the new defaults without losing user edits in the empty-create
  // branch (where sourceRole is undefined forever).
  const defaultValues = useMemo<RoleEditorFormValues>(() => {
    if (mode === 'create' && cloneFromId && sourceRole) {
      return {
        name: t('cloneNamePrefix', { sourceName: sourceRole.name }),
        description: sourceRole.description,
        permissions: sourceRole.permissions.filter((p) => actorPermsSet.has(p)),
      };
    }
    if (mode === 'edit' && sourceRole) {
      return {
        name: sourceRole.name,
        description: sourceRole.description,
        permissions: sourceRole.permissions,
      };
    }
    return { name: '', description: '', permissions: [] };
  }, [mode, cloneFromId, sourceRole, actorPermsSet, t]);

  const form = useForm<RoleEditorFormValues>({
    resolver: zodResolver(roleEditorSchema),
    defaultValues,
  });

  // Re-hydrate when the source role resolves after the initial render
  // (cache miss → fetch). Empty-create has no sourceRole; defaultValues
  // already covers that branch.
  const sourceRoleId = sourceRole?.id;
  useEffect(() => {
    if (!sourceRoleId) return;
    form.reset(defaultValues);
    // We intentionally key on sourceRoleId (not defaultValues / form) — once
    // the source role is in the cache we reset exactly once, then leave the
    // user's edits alone.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceRoleId]);

  const createMut = useCreateRole(businessId);
  const updateMut = useUpdateRole(businessId);

  useUnsavedChangesPrompt(form.formState.isDirty, t('unsavedPrompt'));

  const isPending = createMut.isPending || updateMut.isPending;

  const onSubmit = form.handleSubmit(async (values) => {
    try {
      if (mode === 'create') {
        await createMut.mutateAsync({
          name: values.name,
          description: values.description,
          permissions: values.permissions,
        });
        toast.success(t('createSuccess'));
      } else {
        await updateMut.mutateAsync({
          roleId: roleId as string,
          name: values.name,
          description: values.description,
          permissions: values.permissions,
        });
        toast.success(t('updateSuccess'));
      }
      router.push('/settings/roles');
    } catch (err) {
      toast.error(mapRoleError(err));
    }
  });

  const title = mode === 'create' ? t('newTitle') : t('editTitle');
  const nameError = form.formState.errors.name;
  // Map zod codes to localized messages. Only `name_required` is emitted
  // by the schema today — the default branch keeps the surface honest if
  // a future revision adds a new code without updating this map.
  const nameErrorMessage =
    nameError?.message === 'name_required' ? t('nameRequired') : (nameError?.message ?? null);

  return (
    <TooltipProvider>
      <form
        onSubmit={onSubmit}
        className="space-y-6 px-4 pb-28 pt-6 sm:px-12"
        aria-labelledby="role-editor-title"
      >
        <header className="space-y-2">
          <Link
            href="/settings/roles"
            className="inline-flex items-center gap-1 text-sm text-[var(--ov-ink-soft)] hover:text-[var(--ov-ink)]"
          >
            <ChevronLeft className="h-4 w-4" />
            {t('back')}
          </Link>
          <h1
            id="role-editor-title"
            className="text-2xl font-semibold tracking-tight text-[var(--ov-ink)]"
          >
            {title}
          </h1>
        </header>

        <Card className="space-y-4 p-5">
          <div className="space-y-1.5">
            <Label htmlFor="role-name">{t('nameLabel')}</Label>
            <Input
              id="role-name"
              placeholder={t('namePlaceholder')}
              disabled={isPending}
              aria-invalid={!!nameError}
              {...form.register('name')}
            />
            {nameErrorMessage && (
              <p className="text-sm text-[var(--ov-danger)]" role="alert">
                {nameErrorMessage}
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="role-description">{t('descriptionLabel')}</Label>
            <Textarea
              id="role-description"
              placeholder={t('descriptionPlaceholder')}
              disabled={isPending}
              {...form.register('description')}
            />
          </div>
        </Card>

        <Card className="space-y-4 p-5">
          <Label>{t('permissionsLabel')}</Label>
          <Controller
            name="permissions"
            control={form.control}
            render={({ field }) => (
              <PermissionTree
                value={field.value}
                onChange={field.onChange}
                actorPermissions={actorPermsSet}
                disabled={isPending}
              />
            )}
          />
        </Card>

        <div className="fixed inset-x-0 bottom-0 z-10 flex justify-end gap-2 border-t border-[var(--ov-line)] bg-[var(--ov-paper-raised)] px-4 py-3 sm:px-12 md:left-48">
          <Button asChild variant="ghost" disabled={isPending}>
            <Link href="/settings/roles">{t('cancel')}</Link>
          </Button>
          <Button type="submit" disabled={!form.formState.isDirty || isPending}>
            {t('save')}
          </Button>
        </div>
      </form>
    </TooltipProvider>
  );
}
