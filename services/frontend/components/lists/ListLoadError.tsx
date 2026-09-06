'use client';

import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';

interface ListLoadErrorProps {
  onRetry?: () => void;
}

export function ListLoadError({ onRetry }: ListLoadErrorProps) {
  const tCommon = useTranslations('common');
  return (
    <div className="border-[var(--ov-danger)]/40 flex flex-col items-start gap-4 rounded-lg border bg-[var(--ov-danger-soft)] p-6 text-sm text-[var(--ov-danger)]">
      <p>{tCommon('loadError')}</p>
      {onRetry && (
        <Button variant="danger" size="sm" onClick={onRetry}>
          {tCommon('retry')}
        </Button>
      )}
    </div>
  );
}
