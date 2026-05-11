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
import { mapRoleError } from '@/lib/resolveErrorMap';
import { PermissionTree } from '@/components/permission-tree/PermissionTree';
import { TooltipProvider } from '@/components/ui/tooltip';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';

// `roleEditorSchema` — zod validation for the editor form. The `name` field is
// the only constraint enforced at the form level (1..N chars, trimmed); the
// backend re-validates name length + uniqueness + permission subset on every
// POST/PATCH. Validation messages are zod *codes* (e.g. 'name_required') —
// the i18n layer maps the code to a localized string at render time so the
// schema stays in module scope (no React hook context).
//
// NIT-01 (Phase 5 review): the codes-then-map convention here is DELIBERATE
// and differs from `lib/schemas.ts` where `getTranslator('validation')` feeds
// pre-localized strings directly into zod. The reason: this schema is
// defined at module scope (outside any React component), so it cannot call
// `useTranslations()` to localize at schema build time. Lifting the schema
// inside the component would re-create it on every render — defeating
// react-hook-form's `resolver` identity stability. The codes-then-map
// indirection trades one extra render-time switch for stable schema
// identity. Adding a new code → add a branch to `nameErrorMessage` below.
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
  /** Optional when mode === 'create'. Source role for clone pre-fill (D-04). */
  cloneFromId?: string | null;
}

/**
 * Shared role editor form for `/settings/roles/new` and
 * `/settings/roles/[id]/edit`. Mounted by both route pages with the
 * appropriate `mode`. Layout follows UI-SPEC §S-2:
 *
 *   1. Back link + page title.
 *   2. Card with Name + Description inputs.
 *   3. Card with PermissionTree (wrapped in react-hook-form Controller).
 *   4. Sticky bottom action bar with Cancel link + Save submit button.
 *
 * Locked behaviors:
 *   - D-04 (clone pre-fill): when `mode='create' && cloneFromId` resolves a
 *     source role, the form is pre-filled with `name = «Копия — {sourceName}»`,
 *     `description = sourceDescription`, and `permissions = sourcePerms ∩
 *     actorPerms`. The intersection is a UX affordance — the backend
 *     re-validates via CheckEscalationSubset and returns 403 if the client
 *     attempts to bypass.
 *   - Edit pre-fill: hydrates from the roles cache (no extra fetch); if the
 *     user lands on /edit cold (cache miss), useRoles fetches and a useEffect
 *     re-resets the form when the source role resolves.
 *   - Save button is disabled until `form.formState.isDirty` flips true or
 *     while a mutation is pending.
 *   - On submit success: toast.success + router.push('/settings/roles');
 *     useCreateRole/useUpdateRole already invalidate roles + permissions on
 *     success (Plan 05-04).
 *   - On submit error: toast.error(mapRoleError(err)); form stays open with
 *     the user's input intact.
 *   - Dirty-state guard via `useUnsavedChangesPrompt(isDirty, t('unsavedPrompt'))`.
 *   - PermissionTree leaves render a Tooltip — wrap the whole form in
 *     TooltipProvider so the Radix portals find their context.
 *
 * UI-RBAC-11: no hardcoded permission strings — every permission key flows
 * through the catalog from PermissionTree.
 */
export function RoleEditorForm({ mode, roleId, cloneFromId }: RoleEditorFormProps) {
  const t = useTranslations('roles.editor');
  const router = useRouter();
  const businessId = useBusinessStore((s) => s.activeBusinessId);

  // Roles list is cached by the list page; if the user lands here cold the
  // hook fires its own fetch. Both create (clone source) and edit modes
  // read the source role from the same cache — no double-fetch.
  const { data: rolesList } = useRoles(businessId);
  const sourceId = mode === 'edit' ? roleId : (cloneFromId ?? undefined);
  const sourceRole = sourceId ? rolesList?.find((r) => r.id === sourceId) : undefined;

  // Actor's effective permissions in the active business. Same query key as
  // usePermission so the cache is shared (Phase 4 D-05).
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
