// app/(app)/posts/_components/PostsEmpty.tsx — fallback rendered inside
// the DataTable's `empty` slot when there are no rows.
//
// Two flavours: "no posts at all" vs "no match for current search". The
// search variant uses the shared EmptySearch component so the mono query
// rendering matches mock-states.jsx.
import { FileText } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { EmptySearch } from '@/components/states';

export function PostsEmpty({
  search,
  onResetSearch,
}: {
  search: string;
  onResetSearch: () => void;
}) {
  const tPosts = useTranslations('posts');
  const hasSearch = search.trim().length > 0;
  if (hasSearch) {
    return (
      <div className="px-5 py-6">
        <EmptySearch query={search.trim()} onResetFilters={onResetSearch} />
      </div>
    );
  }
  return (
    <div className="flex flex-col items-center px-6 py-16 text-center">
      <FileText aria-hidden className="mb-3 size-9 text-ink-faint" />
      <p className="text-sm text-ink-mid">{tPosts('emptyState')}</p>
      <p className="mt-1 max-w-xs text-xs text-ink-soft">{tPosts('emptyHint')}</p>
    </div>
  );
}
