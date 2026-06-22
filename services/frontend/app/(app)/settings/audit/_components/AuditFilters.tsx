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
];

interface Props {
  value: TFilters;
  onChange: (next: TFilters) => void;
  businessID: string;
}

export function AuditFilters({ value, onChange, businessID }: Props) {
  const t = useTranslations('audit.filters');
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
      default:
        return c;
    }
  }

  return (
    <div className="flex flex-wrap items-end gap-4">
      {/* Category chips */}
      <fieldset className="flex flex-col gap-1">
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

      {/* Action select (scoped by category) */}
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-ink-soft">{t('actionLabel')}</span>
        <select
          data-testid="action-select"
          value={value.action ?? ''}
          onChange={(e) => onChange({ ...value, action: e.target.value || undefined })}
          className="min-w-[200px] rounded-md border border-line bg-paper-raised px-2 py-1 text-ink"
        >
          <option value="">{t('actionAny')}</option>
          {actions.map((a) => (
            <option key={a} value={a}>
              {tActions(actionToI18nKey(a))}
            </option>
          ))}
        </select>
      </label>

      {/* Date range */}
      <DateRangePicker
        from={value.from}
        to={value.to}
        onChange={(from, to) => onChange({ ...value, from, to })}
      />

      {/* Actor */}
      <ActorAutocomplete
        businessID={businessID}
        value={value.actorID}
        onChange={(actorID) => onChange({ ...value, actorID })}
      />
    </div>
  );
}
