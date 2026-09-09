'use client';

import { useEffect, useRef, useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppInput as Input, AppTextarea as Textarea } from '@/components/design-system/AppInput';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/design-system/AppDialog';
import { contentTemplatesQueryKey, useContentTemplates } from '@/hooks/useContentTemplates';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { usePermission } from '@/lib/hooks/usePermission';
import { shouldApplyRenderedTemplate } from '@/lib/content-template-guard';
import {
  contentTemplateSchema,
  renderedContentTemplateSchema,
  type ContentTemplate,
} from '@/types/content-template';

const MAX_VISIBLE_CHIPS = 6;
const MAX_NAME_RUNES = 80;
const MAX_BODY_BYTES = 16_384;
const MAX_PLACEHOLDERS = 10;
const MAX_VALUE_RUNES = 512;
const MAX_TEMPLATES = 50;
const PLACEHOLDER = /^[A-Za-z][A-Za-z0-9_]{0,31}$/;
const valuesSchema = z.object({ values: z.record(z.string(), z.string()) });
const editorSchema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1)
      .refine((v) => Array.from(v).length <= MAX_NAME_RUNES),
    kind: z.enum(['post', 'review_reply']),
    body: z
      .string()
      .min(1)
      .refine((v) => new TextEncoder().encode(v).length <= MAX_BODY_BYTES),
    placeholders: z.string(),
  })
  .superRefine((value, ctx) => {
    const keys = splitPlaceholders(value.placeholders);
    if (
      keys.length > MAX_PLACEHOLDERS ||
      new Set(keys).size !== keys.length ||
      keys.some((key) => !PLACEHOLDER.test(key))
    )
      ctx.addIssue({ code: 'custom', path: ['placeholders'], message: 'invalid' });
  });
type ValueFields = z.infer<typeof valuesSchema>;
type EditorFields = z.infer<typeof editorSchema>;

function splitPlaceholders(value: string) {
  return value
    .split(',')
    .map((key) => key.trim())
    .filter(Boolean);
}

function referencedPlaceholders(body: string): string[] | null {
  const keys = new Set<string>();
  for (let index = 0; index < body.length; ) {
    if ((body[index] === '{' || body[index] === '}') && body[index + 1] === body[index]) {
      index += 2;
      continue;
    }
    if (body[index] === '}') return null;
    if (body[index] !== '{') {
      index += 1;
      continue;
    }
    const close = body.indexOf('}', index + 1);
    if (close < 0) return null;
    const key = body.slice(index + 1, close);
    if (!PLACEHOLDER.test(key)) return null;
    keys.add(key);
    index = close + 1;
  }
  return [...keys].sort();
}

interface TemplateChipsProps {
  businessId: string | null;
  conversationKey: string;
  disabled: boolean;
  currentInput: string;
  onPrefill: (content: string) => void;
}

export function TemplateChips({
  businessId,
  conversationKey,
  disabled,
  currentInput,
  onPrefill,
}: TemplateChipsProps) {
  const t = useTranslations('contentTemplates');
  const read = usePermission('content.read');
  const create = usePermission('content.create');
  const update = usePermission('content.update');
  const remove = usePermission('content.delete');
  const query = useContentTemplates(businessId, read.allowed);
  const templates = read.allowed && query.isSuccess ? query.data : [];
  const [selected, setSelected] = useState<ContentTemplate | null>(null);
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [editing, setEditing] = useState<ContentTemplate | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const valuesForm = useForm<ValueFields>({
    resolver: zodResolver(valuesSchema),
    defaultValues: { values: {} },
  });
  const editorForm = useForm<EditorFields>({
    resolver: zodResolver(editorSchema),
    defaultValues: { name: '', kind: 'post', body: '', placeholders: '' },
  });
  const queryClient = useQueryClient();
  const mounted = useRef(true);
  const scopeRef = useRef(businessId);
  const conversationRef = useRef(conversationKey);
  const disabledRef = useRef(disabled);
  const inputRef = useRef(currentInput);
  scopeRef.current = businessId;
  conversationRef.current = conversationKey;
  disabledRef.current = disabled;
  inputRef.current = currentInput;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    setSelected(null);
    setLibraryOpen(false);
    setEditing(null);
    setEditorOpen(false);
  }, [businessId, conversationKey]);
  useEffect(() => {
    valuesForm.reset({
      values: Object.fromEntries((selected?.placeholders ?? []).map((key) => [key, ''])),
    });
  }, [selected, valuesForm]);

  const renderMutation = useMutation({
    mutationFn: ({
      scope,
      templateId,
      values,
    }: {
      scope: string;
      chat: string;
      inputBefore: string;
      templateId: string;
      values: Record<string, string>;
    }) =>
      bizApi(scope)
        .post(BIZ_API_PATHS.CONTENT_TEMPLATES.RENDER(templateId), { values })
        .then((r) => renderedContentTemplateSchema.parse(r.data).content),
    onSuccess: (content, variables) => {
      if (
        !shouldApplyRenderedTemplate({
          mounted: mounted.current,
          requestedBusinessId: variables.scope,
          currentBusinessId: scopeRef.current,
          requestedConversationKey: variables.chat,
          currentConversationKey: conversationRef.current,
          inputBeforeRequest: variables.inputBefore,
          currentInput: inputRef.current,
          disabled: disabledRef.current,
        })
      )
        return;
      onPrefill(content);
      setSelected(null);
    },
    onError: (_error, variables) => {
      if (
        mounted.current &&
        scopeRef.current === variables.scope &&
        conversationRef.current === variables.chat
      )
        toast.error(t('renderError'));
    },
  });
  const crud = useMutation({
    mutationFn: async (v: {
      op: 'create' | 'update' | 'delete';
      scope: string;
      id?: string;
      payload?: {
        name: string;
        kind: 'post' | 'review_reply';
        body: string;
        placeholders: string[];
      };
    }) => {
      if (v.op === 'delete')
        return bizApi(v.scope).delete(BIZ_API_PATHS.CONTENT_TEMPLATES.BY_ID(v.id!));
      const response =
        v.op === 'create'
          ? await bizApi(v.scope).post(BIZ_API_PATHS.CONTENT_TEMPLATES.ROOT, v.payload)
          : await bizApi(v.scope).put(BIZ_API_PATHS.CONTENT_TEMPLATES.BY_ID(v.id!), v.payload);
      contentTemplateSchema.parse(response.data);
      return response;
    },
    onSuccess: (_response, v) => {
      void queryClient.invalidateQueries({ queryKey: contentTemplatesQueryKey(v.scope) });
      if (mounted.current && scopeRef.current === v.scope) {
        editorForm.reset();
        setEditing(null);
        setEditorOpen(false);
        toast.success(t(v.op === 'delete' ? 'deleted' : 'saved'));
      }
    },
    onError: (_error, v) => {
      if (mounted.current && scopeRef.current === v.scope) toast.error(t('saveError'));
    },
  });

  function render(template: ContentTemplate, values: Record<string, string>) {
    if (!businessId || disabled || !read.allowed) return;
    renderMutation.mutate({
      scope: businessId,
      chat: conversationKey,
      inputBefore: currentInput,
      templateId: template.id,
      values,
    });
  }
  function choose(template: ContentTemplate) {
    setLibraryOpen(false);
    if (template.placeholders.length === 0) render(template, {});
    else setSelected(template);
  }
  function submitValues(fields: ValueFields) {
    if (!selected) return;
    let invalid = false;
    selected.placeholders.forEach((key) => {
      if (Array.from(fields.values[key] ?? '').length > MAX_VALUE_RUNES) {
        invalid = true;
        valuesForm.setError(`values.${key}`, { message: t('valueError') });
      }
    });
    if (!invalid) render(selected, fields.values);
  }
  function edit(template?: ContentTemplate) {
    const value = template ?? null;
    setEditing(value);
    setEditorOpen(true);
    editorForm.reset(
      value
        ? {
            name: value.name,
            kind: value.kind,
            body: value.body,
            placeholders: value.placeholders.join(', '),
          }
        : { name: '', kind: 'post', body: '', placeholders: '' }
    );
  }
  function submitEditor(fields: EditorFields) {
    if (!businessId || (editing ? !update.allowed : !create.allowed)) return;
    const declared = splitPlaceholders(fields.placeholders).sort();
    const referenced = referencedPlaceholders(fields.body);
    if (!referenced || declared.join('\0') !== referenced.join('\0')) {
      editorForm.setError('placeholders', { message: t('placeholderMismatch') });
      return;
    }
    crud.mutate({
      op: editing ? 'update' : 'create',
      scope: businessId,
      id: editing?.id,
      payload: {
        name: fields.name.trim(),
        kind: fields.kind,
        body: fields.body,
        placeholders: declared,
      },
    });
  }
  if (!read.allowed || !query.isSuccess) return null;
  return (
    <>
      <div className="flex flex-wrap gap-2" aria-label={t('chipsLabel')}>
        {templates.slice(0, MAX_VISIBLE_CHIPS).map((template) => (
          <button
            key={template.id}
            type="button"
            disabled={disabled}
            onClick={() => choose(template)}
            className="text-brand-ink max-w-48 truncate rounded-full border border-brand-soft bg-brand-soft px-3 py-1.5 text-sm transition-colors hover:border-brand disabled:cursor-not-allowed disabled:opacity-50"
          >
            {template.name}
          </button>
        ))}
        <button
          type="button"
          onClick={() => setLibraryOpen(true)}
          className="rounded-full border border-line px-3 py-1.5 text-sm text-ink-mid hover:bg-paper-sunken"
        >
          {t('manage', { count: templates.length })}
        </button>
      </div>
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className="sm:max-w-[440px]">
          <DialogHeader>
            <DialogTitle>{t('fillTitle', { name: selected?.name ?? '' })}</DialogTitle>
          </DialogHeader>
          <form onSubmit={valuesForm.handleSubmit(submitValues)} className="space-y-4">
            {selected?.placeholders.map((key) => (
              <div key={key} className="space-y-1.5">
                <label htmlFor={`template-value-${key}`} className="text-sm font-medium text-ink">
                  {key}
                </label>
                <Input
                  id={`template-value-${key}`}
                  {...valuesForm.register(`values.${key}`)}
                  aria-invalid={!!valuesForm.formState.errors.values?.[key]}
                />
                {valuesForm.formState.errors.values?.[key] && (
                  <p className="text-xs text-danger">
                    {valuesForm.formState.errors.values[key]?.message}
                  </p>
                )}
              </div>
            ))}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setSelected(null)}>
                {t('cancel')}
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={disabled || !read.allowed || renderMutation.isPending}
              >
                {t('prefill')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={libraryOpen} onOpenChange={setLibraryOpen}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-[620px]">
          <DialogHeader>
            <DialogTitle>{t('libraryTitle')}</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            {templates.map((template) => (
              <div
                key={template.id}
                className="flex items-center gap-2 rounded-md border border-line p-2"
              >
                <button
                  type="button"
                  onClick={() => choose(template)}
                  className="min-w-0 flex-1 truncate text-left text-sm text-ink"
                >
                  {template.name}
                </button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={!update.allowed}
                  onClick={() => edit(template)}
                >
                  {t('edit')}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={!remove.allowed || crud.isPending}
                  onClick={() =>
                    businessId && crud.mutate({ op: 'delete', scope: businessId, id: template.id })
                  }
                >
                  {t('delete')}
                </Button>
              </div>
            ))}
          </div>
          <Button
            type="button"
            variant="secondary"
            disabled={!create.allowed || templates.length >= MAX_TEMPLATES}
            onClick={() => edit()}
          >
            {t('newTemplate')}
          </Button>
          {editorOpen && (
            <form
              onSubmit={editorForm.handleSubmit(submitEditor)}
              className="space-y-3 rounded-md border border-line bg-paper-sunken p-3"
            >
              <Input {...editorForm.register('name')} placeholder={t('name')} />
              <select
                {...editorForm.register('kind')}
                className="h-10 w-full rounded-md border border-control bg-paper px-3 text-sm"
              >
                <option value="post">{t('kindPost')}</option>
                <option value="review_reply">{t('kindReview')}</option>
              </select>
              <Textarea {...editorForm.register('body')} rows={5} placeholder={t('body')} />
              <Input {...editorForm.register('placeholders')} placeholder={t('placeholders')} />
              {Object.keys(editorForm.formState.errors).length > 0 && (
                <p className="text-xs text-danger">{t('editorError')}</p>
              )}
              <DialogFooter>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    setEditing(null);
                    setEditorOpen(false);
                    editorForm.reset();
                  }}
                >
                  {t('cancel')}
                </Button>
                <Button
                  type="submit"
                  variant="primary"
                  disabled={crud.isPending || (editing ? !update.allowed : !create.allowed)}
                >
                  {t('save')}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
