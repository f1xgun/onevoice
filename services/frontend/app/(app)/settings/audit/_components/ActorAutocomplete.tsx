'use client';

import { useTranslations } from 'next-intl';
import { useMembers } from '@/lib/hooks/useMembers';

interface Props {
  businessID: string;
  value?: string;
  onChange: (actorID?: string) => void;
}

// ActorAutocomplete is a thin <select> over the active business's
// members. We deliberately use the native control here rather than
// shadcn <Select> + <Command> — for v1 the list is small (single-digit
// to low-double-digit), the native widget is keyboard-accessible by
// default, and it sidesteps the heavier combobox dependency.
export function ActorAutocomplete({ businessID, value, onChange }: Props) {
  const t = useTranslations('audit.filters');
  const { data: members = [] } = useMembers(businessID);
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="text-ink-soft">{t('actorLabel')}</span>
      <select
        data-testid="actor-select"
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value || undefined)}
        className="min-w-[200px] rounded-md border border-line bg-paper-raised px-2 py-1 text-ink"
      >
        <option value="">{t('actorAny')}</option>
        {members.map((m) => (
          <option key={m.user.id} value={m.user.id}>
            {m.user.email}
          </option>
        ))}
      </select>
    </label>
  );
}
