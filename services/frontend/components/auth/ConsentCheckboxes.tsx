// Phase 22-02 — Surface D: two required-consent checkboxes for the
// Register form. Wires the Linen <Checkbox> (Radix-based, exposes
// `onCheckedChange` not a native ref) into react-hook-form via
// <Controller>, so formState.isValid evaluates to false until BOTH
// fields are literal(true). FieldError appears under each checkbox
// independently (UI-SPEC §D state matrix).

'use client';

import { useTranslations } from 'next-intl';
import {
  Controller,
  type Control,
  type FieldErrors,
  type FieldValues,
  type Path,
} from 'react-hook-form';
import { Checkbox } from '@/components/ui/checkbox';
import { FieldError } from '@/components/ui/field-error';
import { ConsentCheckboxLabel } from './ConsentCheckboxLabel';

interface ConsentCheckboxesProps<T extends FieldValues> {
  control: Control<T>;
  errors: FieldErrors<T>;
  tosName: Path<T>;
  pdnName: Path<T>;
}

export function ConsentCheckboxes<T extends FieldValues>({
  control,
  errors,
  tosName,
  pdnName,
}: ConsentCheckboxesProps<T>) {
  const t = useTranslations('register');
  const tosError = errors[tosName];
  const pdnError = errors[pdnName];

  return (
    <fieldset className="mt-2 space-y-4">
      <div>
        <label className="flex cursor-pointer items-start gap-3 py-2">
          <Controller
            control={control}
            name={tosName}
            render={({ field }) => (
              <Checkbox
                checked={field.value === true}
                onCheckedChange={(value) => field.onChange(value === true)}
                onBlur={field.onBlur}
                aria-describedby={tosError ? 'err-tos-privacy' : undefined}
              />
            )}
          />
          <span className="text-[14px] leading-[1.4] text-[var(--ov-ink-mid)]">
            <ConsentCheckboxLabel text={t('consent.tosPrivacy')} />
          </span>
        </label>
        {tosError && <FieldError id="err-tos-privacy">{t('consent.required')}</FieldError>}
      </div>
      <div>
        <label className="flex cursor-pointer items-start gap-3 py-2">
          <Controller
            control={control}
            name={pdnName}
            render={({ field }) => (
              <Checkbox
                checked={field.value === true}
                onCheckedChange={(value) => field.onChange(value === true)}
                onBlur={field.onBlur}
                aria-describedby={pdnError ? 'err-pdn' : undefined}
              />
            )}
          />
          <span className="text-[14px] leading-[1.4] text-[var(--ov-ink-mid)]">
            <ConsentCheckboxLabel text={t('consent.pdn')} />
          </span>
        </label>
        {pdnError && <FieldError id="err-pdn">{t('consent.required')}</FieldError>}
      </div>
    </fieldset>
  );
}
