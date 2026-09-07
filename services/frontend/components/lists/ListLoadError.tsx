'use client';

import { AlertTriangle } from 'lucide-react';
import { StatusLine } from '@/components/design-system/StatusLine';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';

interface ListLoadErrorProps {
  onRetry?: () => void;
}

export function ListLoadError({ onRetry }: ListLoadErrorProps) {
  const tCommon = useTranslations('common');
  return (
    <div className="flex flex-col items-start gap-4 rounded-lg border border-danger bg-paper-raised p-4 text-meta text-danger">
      <StatusLine role="alert" tone="danger" icon={AlertTriangle} text={tCommon('loadError')} />
      {onRetry && (
        <Button variant="danger" size="sm" onClick={onRetry}>
          {tCommon('retry')}
        </Button>
      )}
    </div>
  );
}
