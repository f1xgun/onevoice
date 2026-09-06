'use client';

import { type UseFormReturn } from 'react-hook-form';
import { useTranslations } from 'next-intl';
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import type { FormValues } from './useProjectForm';

interface CreateProjectFieldsProps {
  form: UseFormReturn<FormValues>;
}

// Create flow — only name + description. Остальное настраивается после создания.
export function CreateProjectFields({ form }: CreateProjectFieldsProps) {
  const tForm = useTranslations('projects.form');

  return (
    <>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('name')}</FormLabel>
            <FormControl>
              <Input placeholder={tForm('namePlaceholder')} autoFocus {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="description"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {tForm('description')}{' '}
              <span className="text-muted-foreground">{tForm('optional')}</span>
            </FormLabel>
            <FormControl>
              <Textarea rows={3} placeholder={tForm('descriptionPlaceholder')} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <p className="bg-muted/30 rounded-md border px-4 py-3 text-xs text-muted-foreground">
        {tForm('creationFooter')}
      </p>
    </>
  );
}
