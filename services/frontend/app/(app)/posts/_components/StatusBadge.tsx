// app/(app)/posts/_components/StatusBadge.tsx — status pill for the
// posts table.
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12.
import { POST_STATUS_LABELS } from '@/lib/constants/statuses';
import { Badge } from '@/components/ui/badge';

export function StatusBadge({ status }: { status: string }) {
  const label = POST_STATUS_LABELS[status as keyof typeof POST_STATUS_LABELS] ?? status;
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
