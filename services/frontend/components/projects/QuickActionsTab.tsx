'use client';

import { type UseFormReturn } from 'react-hook-form';
import { useTranslations } from 'next-intl';
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { TabsContent } from '@/components/ui/tabs';
import { QuickActionsEditor } from './QuickActionsEditor';
import type { FormValues } from './useProjectForm';

interface QuickActionsTabProps {
  form: UseFormReturn<FormValues>;
}

export function QuickActionsTab({ form }: QuickActionsTabProps) {
  const tForm = useTranslations('projects.form');

  return (
    <TabsContent value="quick-actions" className="space-y-6 pt-4">
      <FormField
        control={form.control}
        name="quickActions"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('quickActions')}</FormLabel>
            <FormDescription>{tForm('quickActionsDescription')}</FormDescription>
            <FormControl>
              <QuickActionsEditor value={field.value} onChange={field.onChange} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </TabsContent>
  );
}
