'use client';

import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useLocale, useTranslations } from 'next-intl';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { listConversations, createConversation } from '@/lib/conversations';
import { conversationsQueryKey } from '@/hooks/useConversations';
import { useConversationFlow } from '@/hooks/useConversationFlow';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { AppTextarea } from '@/components/design-system/AppInput';
import { TemplateChips } from '@/components/templates/TemplateChips';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { MessageBubble } from '@/components/chat/MessageBubble';
import { ChatPhotoInput } from '@/components/chat/ChatPhotoInput';
import type { Attachment } from '@/types/chat';

const schema = z.object({
  instruction: z.string().trim().min(1),
  attachment: z.custom<Attachment>().optional(),
});
export default function ChatListPage() {
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  return <Instructions key={businessId} businessId={businessId} />;
}
function Instructions({ businessId }: { businessId: string | null }) {
  const t = useTranslations('chat.instructions');
  const locale = useLocale();
  const router = useRouter();
  const qc = useQueryClient();
  const permission = usePermission('content.create');
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const createdId = useRef<string | null>(null);
  const sending = useRef(false);
  const [photoUploading, setPhotoUploading] = useState(false);
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { instruction: '' },
  });
  const flow = useConversationFlow({ conversationId: '' });
  const {
    data: conversations = [],
    isPending,
    isError,
    refetch,
  } = useQuery({
    queryKey: conversationsQueryKey(businessId),
    queryFn: () => listConversations(businessId!),
    enabled: !!businessId,
  });
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get('businessId') === businessId && params.get('prompt'))
      form.setValue('instruction', params.get('prompt')!);
  }, [businessId, form]);
  async function send({ instruction, attachment }: z.infer<typeof schema>) {
    if (!businessId || !permission.allowed || sending.current || photoUploading) return;
    sending.current = true;
    try {
      if (!createdId.current)
        createdId.current = (await createConversation(businessId, { title: t('newTitle') })).id;
      if (!mounted.current || useBusinessStore.getState().activeBusinessId !== businessId) return;
      const id = createdId.current;
      await flow.sendMessage(
        instruction,
        () => {
          form.reset();
          void qc.invalidateQueries({ queryKey: conversationsQueryKey(businessId) });
          if (mounted.current && useBusinessStore.getState().activeBusinessId === businessId)
            router.push(`/chat/${id}`);
        },
        { conversationId: id, attachments: attachment ? [attachment] : undefined }
      );
    } catch {
      form.setError('root', { message: t('sendError') });
    } finally {
      sending.current = false;
    }
  }
  const days = new Map<string, typeof conversations>();
  for (const conversation of conversations
    .filter((row) => row.preview?.trim())
    .sort((a, b) =>
      (b.lastMessageAt ?? b.createdAt).localeCompare(a.lastMessageAt ?? a.createdAt)
    )) {
    const date = (conversation.lastMessageAt ?? conversation.createdAt).slice(0, 10);
    days.set(date, [...(days.get(date) ?? []), conversation]);
  }
  const attachment = form.watch('attachment');
  return (
    <div className="max-w-[816px] p-gutter md:p-gutter-desktop">
      <h1 className="mb-3.5 text-2xl font-semibold">{t('title')}</h1>
      <section className="rounded-block bg-paper-raised p-4 md:p-5">
        <form onSubmit={form.handleSubmit(send)} className="space-y-3">
          <label htmlFor="instruction" className="block font-semibold">
            {t('what')}
          </label>
          <AppTextarea
            id="instruction"
            rows={3}
            placeholder={t('placeholder')}
            disabled={!permission.allowed || flow.isStreaming || form.formState.isSubmitting}
            {...form.register('instruction')}
          />
          {attachment && (
            <p className="flex items-center justify-between gap-3 text-sm text-ink-soft">
              {attachment.name}
              <button type="button" onClick={() => form.setValue('attachment', undefined)}>
                {t('removePhoto')}
              </button>
            </p>
          )}
          <div className="flex justify-end gap-2">
            <ChatPhotoInput
              disabled={!permission.allowed || flow.isStreaming}
              onUploadingChange={setPhotoUploading}
              onChange={(photo) => form.setValue('attachment', photo)}
            />
            <Button
              type="submit"
              disabled={
                !permission.allowed ||
                flow.isStreaming ||
                form.formState.isSubmitting ||
                photoUploading ||
                !form.watch('instruction').trim()
              }
            >
              {t('send')}
            </Button>
          </div>
          {form.formState.errors.root && (
            <p role="alert" className="text-sm text-danger">
              {form.formState.errors.root.message}
            </p>
          )}
        </form>
        {permission.isError && <PermissionLoadError onRetry={permission.refetch} />}
        <h2 className="mb-2.5 mt-3 font-semibold">{t('occasions')}</h2>
        <TemplateChips
          variant="occasions"
          businessId={businessId}
          conversationKey={businessId ?? ''}
          disabled={!permission.allowed || flow.isStreaming}
          currentInput={form.watch('instruction')}
          onPrefill={(text) => form.setValue('instruction', text)}
        />
      </section>
      {flow.messages.length > 0 && (
        <div className="mt-7">
          {flow.messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}
        </div>
      )}
      <div className="mt-7 space-y-7">
        {isError ? (
          <ListLoadError onRetry={refetch} />
        ) : isPending ? (
          <p role="status">{t('loading')}</p>
        ) : days.size === 0 ? (
          <p className="text-[15px] leading-[22px] text-ink-soft">{t('empty')}</p>
        ) : (
          [...days].map(([day, rows]) => (
            <section key={day}>
              <h2 className="mb-2.5 font-semibold">
                {new Date(`${day}T12:00:00`).toLocaleDateString(locale, {
                  day: 'numeric',
                  month: 'long',
                })}
              </h2>
              <div className="space-y-1 rounded-block bg-paper-raised p-2">
                {rows.map((conversation) => (
                  <Link
                    key={conversation.id}
                    href={`/chat/${conversation.id}`}
                    className="flex min-h-20 items-start gap-3 rounded-inner p-3 hover:bg-paper-sunken"
                  >
                    <div className="min-w-0 flex-1">
                      <h3 className="break-words font-semibold">
                        {conversation.titleStatus === 'auto_pending' ||
                        !conversation.title?.trim() ||
                        /^(Новый чат|Новый диалог|Новое поручение|New chat|New conversation|New instruction)$/i.test(
                          conversation.title.trim()
                        )
                          ? t('genericTitle')
                          : conversation.title}
                      </h3>
                      <p className="mt-1 line-clamp-2 text-[15px] leading-[22px] text-ink-soft">
                        {conversation.preview
                          ?.replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
                          .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
                          .replace(/(\*\*|__|~~|`)(.*?)\1/g, '$2')
                          .replace(/(^|\s)\*([^*]+)\*(?=\s|$|[.,!?])/g, '$1$2')
                          .replace(/^\s*(#{1,6}|>)\s+/gm, '')
                          .trim() || t('noOutcome')}
                      </p>
                    </div>
                  </Link>
                ))}
              </div>
            </section>
          ))
        )}
      </div>
    </div>
  );
}
