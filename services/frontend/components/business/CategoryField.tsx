'use client';

// Category selector for the organization profile/create forms.
//
// Presets resolve from `business.categories.*` per locale; the stored value
// is the stable preset id. Picking «Другое» (other) reveals a free-text
// input and the typed text becomes the saved `category` — so a business that
// doesn't fit a preset can still record its real category instead of the
// literal sentinel "other". On edit, a stored category that matches no preset
// is treated as a custom value (the select shows «Другое», the input
// pre-fills with the stored text).

import { useState } from 'react';
import { Controller, type Control, type ControllerRenderProps } from 'react-hook-form';
import { useTranslations } from 'next-intl';

import { Field } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { BusinessInput } from '@/lib/schemas';

const PRESET_CATEGORY_IDS = ['cafe', 'retail', 'service', 'beauty', 'education'] as const;
const OTHER_CATEGORY = 'other';

export interface CategoryFieldProps {
  control: Control<BusinessInput>;
  error?: string;
}

export function CategoryField({ control, error }: CategoryFieldProps) {
  const tProfileForm = useTranslations('business.profileForm');
  const tCategories = useTranslations('business.categories');
  const presets = PRESET_CATEGORY_IDS.map((id) => ({ value: id, label: tCategories(id) }));

  return (
    <Field label={tProfileForm('fields.category')} required error={error}>
      <Controller
        control={control}
        name="category"
        render={({ field }) => (
          <CategoryControl
            field={field}
            presets={presets}
            placeholder={tProfileForm('categoryPlaceholder')}
            otherLabel={tCategories(OTHER_CATEGORY)}
            categoryAria={tProfileForm('fields.category')}
            customPlaceholder={tProfileForm('customCategoryPlaceholder')}
            customAria={tProfileForm('customCategoryAria')}
          />
        )}
      />
    </Field>
  );
}

interface CategoryControlProps {
  field: ControllerRenderProps<BusinessInput, 'category'>;
  presets: ReadonlyArray<{ value: string; label: string }>;
  placeholder: string;
  otherLabel: string;
  categoryAria: string;
  customPlaceholder: string;
  customAria: string;
}

function CategoryControl({
  field,
  presets,
  placeholder,
  otherLabel,
  categoryAria,
  customPlaceholder,
  customAria,
}: CategoryControlProps) {
  const value = field.value ?? '';
  const isPreset = presets.some((p) => p.value === value);
  // `forcedOther` covers the fresh-pick case where the value is cleared to ''
  // (so required validation still bites); the value-based check covers the
  // edit case where an existing custom string loads before any interaction.
  const [forcedOther, setForcedOther] = useState(false);
  const isOther = forcedOther || (value !== '' && !isPreset);
  const selectValue = isOther ? OTHER_CATEGORY : value;

  function handleSelect(next: string) {
    if (next === OTHER_CATEGORY) {
      setForcedOther(true);
      field.onChange('');
    } else {
      setForcedOther(false);
      field.onChange(next);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Select value={selectValue} onValueChange={handleSelect}>
        <SelectTrigger
          id="category"
          aria-label={categoryAria}
          onBlur={field.onBlur}
          ref={field.ref}
        >
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {presets.map((c) => (
            <SelectItem key={c.value} value={c.value}>
              {c.label}
            </SelectItem>
          ))}
          <SelectItem value={OTHER_CATEGORY}>{otherLabel}</SelectItem>
        </SelectContent>
      </Select>
      {isOther && (
        <Input
          value={value}
          onChange={(e) => field.onChange(e.target.value)}
          onBlur={field.onBlur}
          placeholder={customPlaceholder}
          aria-label={customAria}
        />
      )}
    </div>
  );
}
