// app/(app)/posts/_components/MediaThumb.tsx — Linen thumbnail rendered
// inside ExpandedPanel for each attached media file.
//
// Extracted from posts/page.tsx as part of.
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';

import { MonoLabel } from '@/components/ui/mono-label';

export function MediaThumb({ url, index }: { url: string; index: number }) {
  const tPosts = useTranslations('posts');
  const fallback = tPosts('mediaFile', { index: index + 1 });
  const filename = useMemo(() => {
    try {
      const parsed = new URL(url, 'http://example.com');
      return parsed.pathname.split('/').filter(Boolean).pop() ?? fallback;
    } catch {
      return fallback;
    }
  }, [url, fallback]);
  return (
    <div className="flex items-center gap-2.5 rounded-sm bg-paper-sunken px-2.5 py-2">
      <span aria-hidden className="size-8 rounded-sm bg-paper-well" />
      <MonoLabel tone="mid">{filename}</MonoLabel>
    </div>
  );
}
