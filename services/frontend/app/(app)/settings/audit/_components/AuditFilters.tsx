'use client';

import { useTranslations } from 'next-intl';
import type { AuditCategory, AuditFilters as TFilters } from '../_lib/types';
import { actionsForCategory, actionToI18nKey } from '../_lib/actionLabels';
import { DateRangePicker } from './DateRangePicker';
import { ActorAutocomplete } from './ActorAutocomplete';

// Categories rendered as chips. 'all' is the default; the remaining five
// match the audit category prefixes in pkg/audit/actions.go.
const CATEGORIES: ReadonlyArray<AuditCategory | 'all'> = [
  'all',
  'rbac',
  'auth',
  'integration',
  'business',
  'project',
  'rpa',
  'platform',
  'review',
  'hitl',
];

interface Props {
  value: TFilters;
  onChange: (next: TFilters) => void;
  businessID: string;
}

export function AuditFilters({ value, onChange, businessID }: Props) {
  const t = useTranslations('audit.filters');
  const tV2 = useTranslations('settings.v2');
  const tActions = useTranslations();
  const cat = value.category ?? 'all';
  const actions = actionsForCategory(cat);

  function categoryLabel(c: AuditCategory | 'all'): string {
    switch (c) {
      case 'all':
        return t('categoryAll');
      case 'rbac':
        return t('categoryRbac');
      case 'auth':
        return t('categoryAuth');
      case 'integration':
        return t('categoryIntegration');
      case 'business':
        return t('categoryBusiness');
      case 'project':
        return t('categoryProject');
      case 'rpa':
        return t('categoryRpa');
      case 'platform':
        return t('categoryPlatform');
      case 'review':
        return t('categoryReview');
      case 'hitl':
        return t('categoryHitl');
      default:
        return c;
    }
  }

  return (
    <div className="flex flex-wrap items-end gap-4">
      {/* Actor */}
      <ActorAutocomplete
        businessID={businessID}
        value={value.actorID}
        onChange={(actorID) => onChange({ ...value, actorID })}
      />
      {/* Action select (scoped by category) */}
      <label className="flex w-full min-w-0 max-w-full flex-col gap-1 text-sm sm:w-64">
        <span className="text-ink-soft">{t('actionLabel')}</span>
        <select
          data-testid="action-select"
          value={value.action ?? ''}
          onChange={(e) => onChange({ ...value, action: e.target.value || undefined })}
          className="min-h-control w-full min-w-0 max-w-full rounded-inner border border-field bg-block px-3 py-2 text-reading text-ink"
        >
          <option value="">{t('actionAny')}</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {tActions(actionToI18nKey(a))}
            </option>
          ))}
        </select>
      </label>

      <details className="w-full">
        <summary className="min-h-tap cursor-pointer text-meta text-brand">
          {tV2('moreFilters')}
        </summary>
        <div className="flex flex-wrap items-end gap-4">
          {' '}
          {/* Category chips */}
          <fieldset className="flex min-w-0 max-w-full flex-col gap-1">
            <legend className="text-sm text-ink-soft">{t('categoryLabel')}</legend>
            <div role="tablist" aria-label={t('categoryLabel')} className="flex flex-wrap gap-2">
              {CATEGORIES.map((c) => {
                const active = cat === c;
                return (
                  <button
                    key={c}
                    type="button"
                    role="tab"
                    aria-selected={active}
                    data-testid={`cat-chip-${c}`}
                    onClick={() => onChange({ ...value, category: c, action: undefined })}
                    className={
                      active
                        ? 'rounded-full bg-ink px-3 py-1 text-sm text-paper'
                        : 'rounded-full border border-line px-3 py-1 text-sm text-ink hover:bg-paper-sunken'
                    }
                  >
                    {categoryLabel(c)}
                  </button>
                );
              })}
            </div>
          </fieldset>
          {/* Date range */}
          <DateRangePicker
            from={value.from}
            to={value.to}
            onChange={(from, to) => onChange({ ...value, from, to })}
          />
        </div>
      </details>
    </div>
  );
}
