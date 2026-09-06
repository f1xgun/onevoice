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
import { AppTextarea as Textarea } from '@/components/design-system/AppInput';
import { cn } from '@/lib/utils';
import { MAX_SYSTEM_PROMPT_CHARS, type FormValues } from './useProjectForm';

interface PromptTabProps {
  form: UseFormReturn<FormValues>;
  systemPromptLen: number;
  overCap: boolean;
}

export function PromptTab({ form, systemPromptLen, overCap }: PromptTabProps) {
  const tForm = useTranslations('projects.form');

  return (
    <TabsContent value="prompt" className="space-y-6 pt-4">
      <FormField
        control={form.control}
        name="systemPrompt"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('systemPrompt')}</FormLabel>
            <FormDescription>{tForm('systemPromptDescription')}</FormDescription>
            <FormControl>
              <Textarea rows={10} placeholder={tForm('systemPromptPlaceholder')} {...field} />
            </FormControl>
            <div className="flex justify-end">
              <span
                className={cn(
                  'text-xs tabular-nums',
                  overCap ? 'text-destructive' : 'text-muted-foreground'
                )}
                aria-live="polite"
              >
                {tForm('promptCounter', {
                  current: systemPromptLen,
                  max: MAX_SYSTEM_PROMPT_CHARS,
                })}
              </span>
            </div>
            <FormMessage />
          </FormItem>
        )}
      />
    </TabsContent>
  );
}
