// app/(app)/posts/_components/MediaThumb.tsx — Linen thumbnail rendered
// inside ExpandedPanel for each attached media file.
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12.
import { useMemo } from 'react';

import { MonoLabel } from '@/components/ui/mono-label';

export function MediaThumb({ url, index }: { url: string; index: number }) {
  const filename = useMemo(() => {
    try {
      const parsed = new URL(url, 'http://example.com');
      return parsed.pathname.split('/').filter(Boolean).pop() ?? `файл ${index + 1}`;
    } catch {
      return `файл ${index + 1}`;
    }
  }, [url, index]);
  return (
    <div className="flex items-center gap-2.5 rounded-sm bg-paper-sunken px-2.5 py-2">
      <span aria-hidden className="size-8 rounded-sm bg-paper-well" />
      <MonoLabel tone="mid">{filename}</MonoLabel>
    </div>
  );
}
