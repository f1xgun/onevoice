import type { HTMLAttributes } from 'react';
import { Card } from '@/components/ui/card';
import { cn } from '@/lib/utils';

export interface DraftSurfaceProps extends HTMLAttributes<HTMLDivElement> {
  className?: string;
}

export function DraftSurface({ className, children, ...props }: DraftSurfaceProps) {
  return (
    <Card
      {...props}
      className={cn(
        'min-w-0 rounded-lg border-[length:var(--ov-block-border)] border-line bg-card p-4 shadow-none sm:p-[var(--ov-block-pad-desktop)]',
        className
      )}
    >
      <div className="min-w-0 break-words text-reading">{children}</div>
    </Card>
  );
}
