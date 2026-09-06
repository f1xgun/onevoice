'use client';

import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';

export interface PermissionLoadErrorProps {
  /** Re-runs the permission query, e.g. `usePermission(...).refetch`. */
  onRetry: () => void;
}

/**
 * Inline banner for surfaces that lock controls behind a permission check.
 * Rendered when the permission list itself failed to load, so the locked
 * state reads as a retryable failure instead of "no permission".
 */
export function PermissionLoadError({ onRetry }: PermissionLoadErrorProps) {
  const t = useTranslations('permissions');
  return (
    <div
      role="alert"
      className="border-[var(--ov-danger)]/40 flex flex-wrap items-center justify-between gap-3 rounded-md border bg-[var(--ov-danger-soft)] px-4 py-3"
    >
      <p className="text-[13px] text-[var(--ov-danger)]">{t('loadError')}</p>
      <Button type="button" variant="secondary" size="sm" onClick={onRetry}>
        {t('retry')}
      </Button>
    </div>
  );
}
