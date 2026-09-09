'use client';

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { EmptyFrame } from './EmptyFrame';

interface EmptyTasksProps {
  onResetFilters?: () => void;
}

export function EmptyTasks({ onResetFilters }: EmptyTasksProps) {
  const tStates = useTranslations('states.emptyTasks');
  return (
    <EmptyFrame
      title={tStates(onResetFilters ? 'filteredTitle' : 'title')}
      body={tStates(onResetFilters ? 'filteredBody' : 'body')}
      action={
        onResetFilters ? (
          <Button onClick={onResetFilters}>{tStates('resetFilters')}</Button>
        ) : (
          <Button asChild>
            <Link href="/chat">{tStates('openChat')}</Link>
          </Button>
        )
      }
    />
  );
}
