'use client';

import { useEffect, useRef } from 'react';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { ArrowRight } from 'lucide-react';
import { AppTextarea } from '@/components/design-system/AppInput';
import { ActionButton } from '@/components/design-system/ActionButton';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { usePermission } from '@/lib/hooks/usePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { createConversation } from '@/lib/conversations';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { useConversationFlow } from '@/hooks/useConversationFlow';

const schema = z.object({ instruction: z.string().trim().min(1) });

interface InstructionComposerProps {
  businessId: string;
}

export function InstructionComposer({ businessId }: InstructionComposerProps) {
  const t = useTranslations('inbox');
  const tChat = useTranslations('chat.instructions');
  const permission = usePermission('content.create');
  const router = useRouter();
  const qc = useQueryClient();
  const flow = useConversationFlow({ conversationId: '' });
  const createdId = useRef<string | null>(null);
  const sending = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { instruction: '' },
  });
  const submit = form.handleSubmit(async ({ instruction }) => {
    if (!permission.allowed || permission.isError || permission.isLoading || sending.current)
      return;
    sending.current = true;
    try {
      if (!createdId.current)
        createdId.current = (await createConversation(businessId, { title: tChat('newTitle') })).id;
      if (!mounted.current || useBusinessStore.getState().activeBusinessId !== businessId) return;
      const id = createdId.current;
      let accepted = false;
      await flow.sendMessage(
        instruction,
        () => {
          accepted = true;
          void qc.invalidateQueries({ queryKey: conversationsQueryKey(businessId) });
          if (mounted.current && useBusinessStore.getState().activeBusinessId === businessId) {
            form.reset();
            router.push(`/chat/${id}`);
          }
        },
        { conversationId: id }
      );
      if (!accepted && mounted.current) form.setError('root', { message: t('instructionError') });
    } catch {
      form.setError('root', { message: t('instructionError') });
    } finally {
      sending.current = false;
    }
  });
  return (
    <form onSubmit={submit} className="rounded-block bg-block p-3">
      <div className="flex items-center gap-3">
        <AppTextarea
          {...form.register('instruction')}
          rows={2}
          aria-label={t('instruct')}
          placeholder={t('instructionPlaceholder')}
          disabled={form.formState.isSubmitting || !permission.allowed}
          className="!min-h-11 resize-none !border-0 !bg-transparent !p-2 !shadow-none"
        />
        <ActionButton
          type="submit"
          size="icon"
          aria-label={t('instruct')}
          disabled={
            !permission.allowed ||
            permission.isError ||
            permission.isLoading ||
            form.formState.isSubmitting ||
            !form.watch('instruction').trim()
          }
        >
          <ArrowRight aria-hidden className="h-5 w-5" />
        </ActionButton>
      </div>
      {permission.isError && <PermissionLoadError onRetry={permission.refetch} />}
      {form.formState.errors.root && (
        <p role="alert" className="mt-2 text-meta text-danger">
          {t('instructionError')}
        </p>
      )}
    </form>
  );
}
