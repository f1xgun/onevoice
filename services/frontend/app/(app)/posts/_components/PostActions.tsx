'use client';

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import Link from 'next/link';
import { bizApi } from '@/lib/api/business-api';
import { useBusinessStore } from '@/lib/stores/business';
import { useBusinessList } from '@/lib/hooks/useBusinessList';
import { usePermission } from '@/lib/hooks/usePermission';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { format } from 'date-fns';
import { AppInput, AppTextarea } from '@/components/design-system/AppInput';
import {
  Dialog,
  AppDialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/design-system/AppDialog';
import type { Post } from '@/types/post';

const OWNER_ROLE_ID = '00000000-0000-0000-0000-000000000001';
const schema = z.object({
  content: z.string().trim().min(1),
  scheduledAt: z
    .string()
    .optional()
    .refine(
      (value) =>
        !value ||
        (Number.isFinite(new Date(value).getTime()) && new Date(value).getTime() > Date.now())
    ),
});
interface PostActionsProps {
  posts: Post[];
}

export function PostActions({ posts }: PostActionsProps) {
  const t = useTranslations('posts');
  const common = useTranslations('common');
  const businessId = useBusinessStore((s) => s.activeBusinessId);
  const membershipQuery = useBusinessList();
  const membership = membershipQuery.data?.find((b) => b.id === businessId);
  const editPermission = usePermission('content.update');
  const createPermission = usePermission('content.create');
  const deletePermission = usePermission('content.delete');
  const canEdit = editPermission.allowed;
  const canCreate = createPermission.allowed;
  const canDelete = deletePermission.allowed && membership?.role.id === OWNER_ROLE_ID;
  const [target, setTarget] = useState<{ post: Post; operation: 'edit' | 'delete' } | null>(null);
  const qc = useQueryClient();
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { content: '', scheduledAt: '' },
  });
  const mutation = useMutation({
    mutationFn: async ({
      post,
      operation,
      content,
      scheduledAt,
    }: {
      post: Post;
      operation: 'edit' | 'delete' | 'retry';
      content?: string;
      scheduledAt?: string;
    }) => {
      const path = `/posts/${post.id}`;
      if (operation === 'edit')
        return (
          await bizApi(businessId!).patch<Post>(path, {
            content,
            ...(scheduledAt ? { scheduledAt } : {}),
          })
        ).data;
      if (operation === 'delete') return (await bizApi(businessId!).delete<Post>(path)).data;
      return (await bizApi(businessId!).post<Post>(`${path}/retry`)).data;
    },
    onSuccess: (post) => {
      if (
        post.status !== 'uncertain' &&
        !Object.values(post.platformResults ?? {}).some((result) => result.error)
      )
        setTarget(null);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ['businesses', businessId, 'posts'] }),
  });
  const problem =
    mutation.isError ||
    (mutation.data &&
      (mutation.data.status === 'uncertain' ||
        Object.values(mutation.data.platformResults ?? {}).some((result) => result.error)));

  const problemMessage =
    Object.values(mutation.data?.platformResults ?? {}).find((result) => result.error)?.error ??
    t('feed.operationProblem');

  function open(post: Post, operation: 'edit' | 'delete') {
    mutation.reset();
    form.reset({
      content: post.content,
      scheduledAt:
        post.status === 'scheduled' && post.scheduledAt
          ? format(new Date(post.scheduledAt), "yyyy-MM-dd'T'HH:mm")
          : '',
    });
    setTarget({ post, operation });
  }

  if (
    [editPermission, createPermission, deletePermission].some((permission) => permission.isError) ||
    membershipQuery.isError
  ) {
    return (
      <PermissionLoadError
        onRetry={() => {
          editPermission.refetch();
          void membershipQuery.refetch();
        }}
      />
    );
  }
  if (
    [editPermission, createPermission, deletePermission].some(
      (permission) => permission.isLoading
    ) ||
    membershipQuery.isLoading
  ) {
    return (
      <p role="status" className="text-sm text-ink-soft">
        {common('loading')}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {posts.map((post) => {
        const results = Object.entries(post.platformResults ?? {});
        const platform = results[0]?.[0];
        const label =
          platform && t.has(`platformShort.${platform}`)
            ? t(`platformShort.${platform}`)
            : platform;
        const hasPublishedTarget = results.some(([, r]) => !!r.postId);
        const supported =
          results.length === 1 &&
          ['telegram', 'vk'].includes(platform) &&
          !!results[0][1].externalId;
        return (
          <div key={post.id} className="flex flex-col gap-2">
            {canCreate &&
              supported &&
              post.status === 'error' &&
              results[0][1].status === 'error' &&
              !hasPublishedTarget && (
                <Button
                  variant="secondary"
                  disabled={mutation.isPending}
                  onClick={() => {
                    mutation.reset();
                    mutation.mutate({ post, operation: 'retry' });
                  }}
                >
                  {t('feed.retryPlatform', { platform: label })}
                </Button>
              )}
            {supported &&
              ((post.status === 'published' && hasPublishedTarget) ||
                (post.status === 'scheduled' && platform === 'telegram')) && (
                <>
                  {canEdit && (
                    <Button
                      variant="secondary"
                      onClick={() => open(post, 'edit')}
                      disabled={mutation.isPending}
                    >
                      {post.status === 'scheduled'
                        ? t('feed.reschedule')
                        : t('feed.editPlatform', { platform: label })}
                    </Button>
                  )}
                  {canDelete && (
                    <Button
                      variant="secondary"
                      className="text-danger"
                      onClick={() => open(post, 'delete')}
                      disabled={mutation.isPending}
                    >
                      {post.status === 'scheduled'
                        ? t('feed.cancelScheduled')
                        : t('feed.deletePlatform', { platform: label })}
                    </Button>
                  )}
                </>
              )}
            {post.status === 'processing' && (
              <p role="status" className="text-sm text-ink-soft">
                {t('feed.processingHelp')}
              </p>
            )}
            {post.status === 'uncertain' && (
              <p role="status" className="text-sm text-ink-soft">
                {t('feed.uncertainHelp')}
              </p>
            )}
            {post.status === 'scheduled' && platform !== 'telegram' && (
              <p className="text-sm text-ink-soft">{t('feed.schedulingHelp')}</p>
            )}
          </div>
        );
      })}
      {canCreate && (
        <Button asChild variant="secondary">
          <Link
            href={`/chat?${new URLSearchParams({ prompt: posts[0]?.content ?? '', businessId: businessId ?? '' })}`}
          >
            {t('feed.repeat')}
          </Link>
        </Button>
      )}
      {problem && (
        <p role="alert" className="text-sm text-danger">
          {problemMessage}
        </p>
      )}
      <Dialog
        open={!!target}
        onOpenChange={(open) => !open && !mutation.isPending && setTarget(null)}
      >
        <AppDialog>
          <DialogHeader>
            <DialogTitle>
              {target?.operation === 'delete'
                ? target.post.status === 'scheduled'
                  ? t('feed.cancelTitle')
                  : t('feed.deleteTitle')
                : t('feed.editTitle')}
            </DialogTitle>
            <DialogDescription>
              {target?.operation === 'delete'
                ? target.post.status === 'scheduled'
                  ? t('feed.cancelHelp')
                  : t('feed.deleteHelp')
                : t('feed.editHelp')}
            </DialogDescription>
          </DialogHeader>
          {target?.operation === 'edit' ? (
            <form
              id="post-edit"
              onSubmit={form.handleSubmit(({ content, scheduledAt }) => {
                if (!target || mutation.isPending || mutation.data?.status === 'uncertain') return;
                if (target.post.status === 'scheduled' && !scheduledAt) {
                  form.setError('scheduledAt', { message: t('feed.futureTime') });
                  return;
                }
                mutation.mutate({
                  ...target,
                  content,
                  ...(target.post.status === 'scheduled' && scheduledAt
                    ? { scheduledAt: new Date(scheduledAt).toISOString() }
                    : {}),
                });
              })}
            >
              {target.post.status === 'scheduled' && (
                <label className="mb-3 block text-sm">
                  {t('feed.publishAt')}
                  <AppInput
                    type="datetime-local"
                    aria-label={t('feed.publishAt')}
                    {...form.register('scheduledAt')}
                  />
                  {form.formState.errors.scheduledAt && (
                    <span role="alert" className="text-danger">
                      {t('feed.futureTime')}
                    </span>
                  )}
                </label>
              )}
              <AppTextarea
                aria-label={t('feed.contentLabel')}
                rows={7}
                {...form.register('content')}
              />
              <p
                role={form.formState.errors.content ? 'alert' : undefined}
                className="text-sm text-danger"
              >
                {form.formState.errors.content && t('feed.contentRequired')}
              </p>
            </form>
          ) : (
            <p className="whitespace-pre-wrap text-[15px] leading-[22px]">{target?.post.content}</p>
          )}
          {problem && (
            <p role="alert" className="text-sm text-danger">
              {problemMessage}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="secondary"
              onClick={() => setTarget(null)}
              disabled={mutation.isPending}
            >
              {common('cancel')}
            </Button>
            <Button
              type={target?.operation === 'edit' ? 'submit' : 'button'}
              form={target?.operation === 'edit' ? 'post-edit' : undefined}
              disabled={mutation.isPending || mutation.data?.status === 'uncertain'}
              onClick={() => target?.operation === 'delete' && mutation.mutate(target)}
            >
              {target?.operation === 'delete'
                ? target.post.status === 'scheduled'
                  ? t('feed.cancelScheduled')
                  : t('feed.confirmDelete')
                : t('feed.save')}
            </Button>
          </DialogFooter>
        </AppDialog>
      </Dialog>
    </div>
  );
}
