'use client';

import { useEffect, useRef, useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input } from '@/components/design-system/AppInput';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/design-system/AppDialog';
import { contentTemplatesQueryKey } from '@/hooks/useContentTemplates';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

const MAX_TEMPLATE_NAME_RUNES = 80;
const schema = z.object({
  name: z
    .string()
    .trim()
    .min(1)
    .refine((value) => Array.from(value).length <= MAX_TEMPLATE_NAME_RUNES),
});
type Fields = z.infer<typeof schema>;

interface SaveTemplateDialogProps {
  businessId: string | null;
  sourceId: string;
  source: 'post' | 'review';
  disabled?: boolean;
}

export function SaveTemplateDialog({
  businessId,
  sourceId,
  source,
  disabled,
}: SaveTemplateDialogProps) {
  const t = useTranslations('contentTemplates');
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const mounted = useRef(true);
  const scopeRef = useRef(businessId);
  const sourceRef = useRef(`${source}:${sourceId}`);
  scopeRef.current = businessId;
  sourceRef.current = `${source}:${sourceId}`;
  const form = useForm<Fields>({ resolver: zodResolver(schema), defaultValues: { name: '' } });
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    if (!open) form.reset();
  }, [form, open]);
  useEffect(() => {
    setOpen(false);
    form.reset();
  }, [businessId, form, source, sourceId]);

  const mutation = useMutation({
    mutationFn: ({
      scope,
      name,
      sourceType,
      itemId,
    }: {
      scope: string;
      name: string;
      sourceType: 'post' | 'review';
      itemId: string;
    }) => {
      const path =
        sourceType === 'post'
          ? BIZ_API_PATHS.CONTENT_TEMPLATES.FROM_POST(itemId)
          : BIZ_API_PATHS.CONTENT_TEMPLATES.FROM_REVIEW(itemId);
      return bizApi(scope).post(path, { name });
    },
    onSuccess: (_response, variables) => {
      void queryClient.invalidateQueries({ queryKey: contentTemplatesQueryKey(variables.scope) });
      if (
        mounted.current &&
        variables.scope === scopeRef.current &&
        `${variables.sourceType}:${variables.itemId}` === sourceRef.current
      ) {
        toast.success(t('saved'));
        setOpen(false);
      }
    },
    onError: (_error, variables) => {
      if (
        mounted.current &&
        variables.scope === scopeRef.current &&
        `${variables.sourceType}:${variables.itemId}` === sourceRef.current
      )
        toast.error(t('saveError'));
    },
  });

  function submit(fields: Fields) {
    if (!businessId || disabled) return;
    mutation.mutate({
      scope: businessId,
      name: fields.name.trim(),
      sourceType: source,
      itemId: sourceId,
    });
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        className="min-h-11"
        disabled={disabled || !businessId}
        onClick={() => setOpen(true)}
      >
        {t('saveAction')}
      </Button>
      <DialogContent className="sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{t('saveTitle')}</DialogTitle>
          <DialogDescription>{t('saveDescription')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(submit)} className="space-y-4">
          <div className="space-y-1.5">
            <label htmlFor={`template-name-${sourceId}`} className="text-sm font-medium text-ink">
              {t('name')}
            </label>
            <Input
              id={`template-name-${sourceId}`}
              {...form.register('name')}
              className="min-h-11"
              autoFocus
            />
            {form.formState.errors.name && <p className="text-xs text-danger">{t('nameError')}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              {t('cancel')}
            </Button>
            <Button type="submit" variant="primary" disabled={disabled || mutation.isPending}>
              {mutation.isPending ? t('saving') : t('save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
