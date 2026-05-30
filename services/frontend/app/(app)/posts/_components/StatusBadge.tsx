// app/(app)/posts/_components/StatusBadge.tsx — status pill for the
// posts table.
//
// Extracted from posts/page.tsx as part of.
'use client';

import { usePostStatusLabels } from '@/lib/constants/statuses';
import { Badge } from '@/components/ui/badge';

export function StatusBadge({ status }: { status: string }) {
  const labels = usePostStatusLabels();
  const label = labels[status as keyof typeof labels] ?? status;
  switch (status) {
    case 'published':
      return (
        <Badge tone="success" dot>
          {label}
        </Badge>
      );
    case 'scheduled':
      return (
        <Badge tone="info" dot>
          {label}
        </Badge>
      );
    case 'error':
      return (
        <Badge tone="danger" dot>
          {label}
        </Badge>
      );
    case 'draft':
    default:
      return (
        <Badge tone="neutral" dot>
          {label}
        </Badge>
      );
  }
}
