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
import type { Tool } from '@/lib/schemas';
import { ProjectApprovalOverrides } from './ProjectApprovalOverrides';
import { ToolCheckboxGrid } from './ToolCheckboxGrid';
import { WhitelistRadio } from './WhitelistRadio';
import type { FormValues } from './useProjectForm';

interface ToolsTabProps {
  form: UseFormReturn<FormValues>;
  whitelistMode: FormValues['whitelistMode'];
  activePlatforms: string[];
  tools: Tool[] | undefined;
  businessApprovals: Record<string, 'auto' | 'manual'>;
}

export function ToolsTab({
  form,
  whitelistMode,
  activePlatforms,
  tools,
  businessApprovals,
}: ToolsTabProps) {
  const tForm = useTranslations('projects.form');

  return (
    <TabsContent value="tools" className="space-y-6 pt-4">
      <FormField
        control={form.control}
        name="whitelistMode"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('tools')}</FormLabel>
            <FormDescription>{tForm('toolsDescription')}</FormDescription>
            <FormControl>
              <WhitelistRadio
                value={field.value}
                onChange={field.onChange}
                name={field.name}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {whitelistMode === 'explicit' && (
        <FormField
          control={form.control}
          name="allowedTools"
          render={({ field }) => (
            <FormItem>
              <FormLabel className="sr-only">{tForm('toolsList')}</FormLabel>
              <FormControl>
                <ToolCheckboxGrid
                  activeIntegrations={activePlatforms}
                  value={field.value}
                  onChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={form.control}
        name="approvalOverrides"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{tForm('approval')}</FormLabel>
            <FormDescription>{tForm('approvalDescription')}</FormDescription>
            <FormControl>
              {tools ? (
                <ProjectApprovalOverrides
                  tools={tools}
                  businessApprovals={businessApprovals}
                  value={field.value}
                  onChange={field.onChange}
                />
              ) : (
                <p className="text-sm text-muted-foreground">{tForm('loadingTools')}</p>
              )}
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </TabsContent>
  );
}
