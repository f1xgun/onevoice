'use client';

import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Form } from '@/components/ui/form';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { BasicsTab } from './BasicsTab';
import { CreateProjectFields } from './CreateProjectFields';
import { DeleteProjectDialog } from './DeleteProjectDialog';
import { PromptTab } from './PromptTab';
import { QuickActionsTab } from './QuickActionsTab';
import { ToolsTab } from './ToolsTab';
import { useProjectForm } from './useProjectForm';
import { usePermission } from '@/lib/hooks/usePermission';
import type { Project } from '@/types/project';

interface ProjectFormProps {
  project?: Project;
  onSaved: (saved: Project) => void;
}

export function ProjectForm({ project, onSaved }: ProjectFormProps) {
  const router = useRouter();
  const tForm = useTranslations('projects.form');
  const tCommon = useTranslations('common');
  const {
    form,
    isEdit,
    submitting,
    systemPromptLen,
    overCap,
    whitelistMode,
    activePlatforms,
    tools,
    businessApprovals,
    chatCount,
    deleteOpen,
    setDeleteOpen,
    isDeletePending,
    onSubmit,
    handleDelete,
  } = useProjectForm(project, onSaved);
  const canCreate = usePermission('content.create').allowed;
  const canUpdate = usePermission('content.update').allowed;
  const canDelete = usePermission('content.delete').allowed;
  const canSubmit = isEdit ? canUpdate : canCreate;

  return (
    <Form {...form}>
      <form onSubmit={onSubmit} className="space-y-6">
        {isEdit ? (
          <Tabs defaultValue="basics" className="w-full">
            {/* Tabs scroll horizontally on narrow viewports — «Быстрые
                действия» otherwise clips off-screen on phones. */}
            <div className="-mx-1 overflow-x-auto px-1 sm:mx-0 sm:overflow-visible sm:px-0">
              <TabsList className="justify-start sm:w-full">
                <TabsTrigger value="basics">{tForm('tabBasics')}</TabsTrigger>
                <TabsTrigger value="prompt">{tForm('tabPrompt')}</TabsTrigger>
                <TabsTrigger value="tools">{tForm('tabTools')}</TabsTrigger>
                <TabsTrigger value="quick-actions">{tForm('tabQuickActions')}</TabsTrigger>
              </TabsList>
            </div>

            <BasicsTab form={form} />
            <PromptTab form={form} systemPromptLen={systemPromptLen} overCap={overCap} />
            <ToolsTab
              form={form}
              whitelistMode={whitelistMode}
              activePlatforms={activePlatforms}
              tools={tools}
              businessApprovals={businessApprovals}
            />
            <QuickActionsTab form={form} />
          </Tabs>
        ) : (
          <CreateProjectFields form={form} />
        )}

        <div className="flex flex-wrap items-center gap-3 pt-2">
          {canSubmit && (
            <Button type="submit" disabled={submitting}>
              {isEdit ? tForm('save') : tForm('create')}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
            disabled={submitting}
          >
            {tCommon('cancel')}
          </Button>
          {isEdit && project && canDelete && (
            <Button
              type="button"
              variant="outline"
              className="hover:bg-destructive/10 ml-auto text-destructive hover:text-destructive"
              onClick={() => setDeleteOpen(true)}
              disabled={submitting || isDeletePending}
            >
              {tForm('deleteProject')}
            </Button>
          )}
        </div>
      </form>

      {isEdit && project && (
        <DeleteProjectDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          projectName={project.name}
          chatCount={chatCount}
          onConfirm={handleDelete}
        />
      )}
    </Form>
  );
}
