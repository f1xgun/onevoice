// app/(app)/posts/_components/ExpandedPanel.tsx — panel rendered when
// the user expands a row in the posts table. Hosts the full content,
// media thumbs, failure-strip + retry, per-platform results, and the
// duplicate / open-link actions.
//
// Extracted from posts/page.tsx as part of.
import { useTranslations } from 'next-intl';

import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

import { firstLink, topLevelErrorStatus, type PostRow } from '../_helpers';
import { MediaThumb } from './MediaThumb';
import { PlatformResultCard } from './PlatformResultCard';

export function ExpandedPanel({ post }: { post: PostRow }) {
  const tPosts = useTranslations('posts');
  const results =
    post.broadcastChannels?.map((c) => [c.platform, c.result] as const) ??
    (post.platformResults ? Object.entries(post.platformResults) : []);
  const firstError = results.find(([, r]) => r.error);
  const failureMessage =
    firstError?.[1].error ?? (topLevelErrorStatus(post) ? tPosts('errorFallback') : null);

  return (
    <div className="grid grid-cols-1 gap-6 px-[60px] pb-5 lg:grid-cols-[1fr_300px]">
      <div className="rounded-md border border-line-soft bg-paper p-4">
        <div className="whitespace-pre-wrap text-sm leading-relaxed text-ink">{post.content}</div>

        {post.mediaUrls && post.mediaUrls.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-2">
            {post.mediaUrls.map((url, i) => (
              <MediaThumb key={`${url}-${i}`} url={url} index={i} />
            ))}
          </div>
        )}

        {failureMessage && (
          <div className="mt-3 flex items-center gap-3 rounded-sm border border-[var(--ov-danger-soft)] bg-[var(--ov-danger-soft)] px-3.5 py-2.5">
            <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-[var(--ov-danger)]" />
            <span className="flex-1 text-sm text-[var(--ov-danger)]">{failureMessage}</span>
            {/* Disabled until POST /posts/:id/retry exists. Wrapper span
                is the tooltip trigger because disabled <button>s don't
                fire pointer events. */}
            <TooltipProvider delayDuration={150}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span tabIndex={0} className="inline-flex">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled
                      aria-disabled
                      className="text-[var(--ov-danger)]"
                    >
                      {tPosts('retry')}
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent>{tPosts('retryUnavailable')}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2.5">
        <MonoLabel>{tPosts('table.results')}</MonoLabel>
        {results.length === 0 ? (
          <div className="rounded-sm border border-line-soft bg-paper px-3 py-2 text-xs text-ink-soft">
            {tPosts('noStats')}
          </div>
        ) : (
          results.map(([platform, result], i) => (
            <PlatformResultCard key={`${platform}-${i}`} platform={platform} result={result} />
          ))
        )}
        <div className="mt-1 flex flex-wrap gap-2">
          <Button variant="secondary" size="sm">
            {tPosts('duplicate')}
          </Button>
          {firstLink(post) && (
            <Button variant="ghost" size="sm" asChild>
              <a href={firstLink(post) ?? undefined} target="_blank" rel="noopener noreferrer">
                {tPosts('openLink')}
              </a>
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
