// app/(app)/posts/_components/PostsSkeleton.tsx — loading skeleton
// rendered inside the DataTable's `skeleton` slot. Matches the
// posts-table grid template so the page doesn't reflow when data lands.
//
// Extracted from posts/page.tsx as part of.
import { Skeleton } from '@/components/ui/skeleton';

const SKELETON_ROW_COUNT = 5;

export function PostsSkeleton() {
  return (
    <div className="divide-y divide-line-soft">
      {Array.from({ length: SKELETON_ROW_COUNT }, (_, i) => (
        <div
          key={i}
          className="grid min-w-[670px] grid-cols-[24px_1fr_190px_200px_160px_56px] items-center gap-4 px-5 py-4"
        >
          <span aria-hidden />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-5 w-20 rounded-full" />
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-4 w-28" />
          <span aria-hidden />
        </div>
      ))}
    </div>
  );
}
