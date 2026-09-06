'use client';

import { type UseFormReturn } from 'react-hook-form';
import { useTranslations } from 'next-intl';
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { AppInput as Input } from '@/components/design-system/AppInput';
import { TabsContent } from '@/components/ui/tabs';
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import type { FormValues } from './useProjectForm';

interface BasicsTabProps {
  form: UseFormReturn<FormValues>;
}

export function BasicsTab({ form }: BasicsTabProps) {
  const tForm = useTranslations('projects.form');

  return (
    <TabsContent value="basics" className="space-y-6 pt-4">
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('name')}</FormLabel>
            <FormControl>
              <Input placeholder={tForm('namePlaceholder')} {...field} />
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
    </TabsContent>
  );
}
