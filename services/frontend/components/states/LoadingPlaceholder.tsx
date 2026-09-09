'use client';

import type { HTMLAttributes } from 'react';

import { useDelayedLoading } from '@/hooks/useDelayedLoading';
import { cn } from '@/lib/utils';

interface LoadingPlaceholderProps extends HTMLAttributes<HTMLDivElement> {
  as?: 'div' | 'span' | 'section' | 'p';
}

/** Mount only while loading. Hidden geometry reserves space during quick requests. */
export function LoadingPlaceholder({
  as: Tag = 'div',
  className,
  children,
  ...props
}: LoadingPlaceholderProps) {
  const visible = useDelayedLoading(true);
  return (
    <Tag
      {...props}
      data-loading-placeholder={visible ? 'visible' : 'pending'}
      data-ov-motion
      aria-hidden={!visible || undefined}
      className={cn(className, visible ? 'motion-safe:animate-page-enter' : 'invisible')}
    >
      {children}
    </Tag>
  );
}
