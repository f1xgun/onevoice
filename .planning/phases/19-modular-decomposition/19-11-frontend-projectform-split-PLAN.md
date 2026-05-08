---
plan: 19-11
phase: 19
slug: frontend-projectform-split
wave: 5
depends_on: []
files_modified:
  - services/frontend/components/projects/ProjectForm.tsx
files_created:
  - services/frontend/components/projects/useProjectForm.ts
  - services/frontend/components/projects/BasicsTab.tsx
  - services/frontend/components/projects/PromptTab.tsx
  - services/frontend/components/projects/ToolsTab.tsx
  - services/frontend/components/projects/QuickActionsTab.tsx
files_deleted: []
success_criteria: [SC-01, SC-03]
autonomous: true
estimated_loc_delta: -360 / +420
---

## Plan Goal

Split `services/frontend/components/projects/ProjectForm.tsx` (409 LOC) into:

- `useProjectForm.ts` — full `useForm<FormValues>` instance + Zod schema + watches + queries (`useTools`, `useBusinessToolApprovals`) + mutations + `onSubmit` + `onDelete`. Single source of truth for form state.
- `BasicsTab.tsx`, `PromptTab.tsx`, `ToolsTab.tsx`, `QuickActionsTab.tsx` — dumb components rendering fields. Receive `form: UseFormReturn<FormValues>` and any computed view-state (`whitelistMode`, `activePlatforms`, `tools`, `businessApprovals`).
- `ProjectForm.tsx` — slim shell (~120 LOC): form provider + `<Tabs>` + 4 tab components + action buttons + delete dialog.

Implements: D-20 (full-form-state hook + dumb tabs).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/frontend/components/projects/ProjectForm.tsx
@services/frontend/AGENTS.md
@docs/frontend-style.md
@docs/frontend-patterns.md
</context>

<tasks>

<task type="auto">
  <id>19-11-01</id>
  <title>Extract useProjectForm hook with full form state + queries + mutations</title>
  <wave>1</wave>
  <read_first>
    - services/frontend/components/projects/ProjectForm.tsx (full file — 409 LOC)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 10 "Frontend: ProjectForm 4-tab split" lines 1267-1358)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-11" lines 1087-1168)
  </read_first>
  <action>
    Create `services/frontend/components/projects/useProjectForm.ts` with the hook signature:

    ```ts
    import { useForm, type UseFormReturn } from 'react-hook-form';
    import { zodResolver } from '@hookform/resolvers/zod';
    import { z } from 'zod';
    // ... import useCreateProject, useUpdateProject, useDeleteProject (existing in @/hooks)
    // ... import useTools, useBusinessToolApprovals
    // ... import useActivePlatforms (or whatever provides activePlatforms today)

    // Zod schema — LIFT VERBATIM from ProjectForm.tsx:46-65 including the
    // .refine() for explicit-whitelist (lines 62-65). Single source of truth
    // for validation; submit handler validates the whole form.
    const MAX_SYSTEM_PROMPT_CHARS = /* lift constant */;

    const schema = z.object({ /* lift verbatim */ }).refine(/* lift verbatim */);

    export type FormValues = z.infer<typeof schema>;
    export type ProjectApprovalOverridesMap = /* lift verbatim from existing types */;

    export interface UseProjectFormResult {
      form: UseFormReturn<FormValues>;
      isEdit: boolean;
      submitting: boolean;
      systemPromptLen: number;
      whitelistMode: FormValues['whitelistMode'];
      activePlatforms: string[];
      tools: ToolEntry[] | undefined;
      businessApprovals: Record<string, 'auto' | 'manual'>;
      chatCount: number;
      onSubmit: () => Promise<void>;
      onDelete: () => Promise<void>;
    }

    export function useProjectForm(
      project: Project | undefined,
      onSaved: (saved: Project) => void
    ): UseProjectFormResult {
      const isEdit = !!project;

      const form = useForm<FormValues>({
        resolver: zodResolver(schema),
        defaultValues: {
          name: project?.name ?? '',
          description: project?.description ?? '',
          systemPrompt: project?.systemPrompt ?? '',
          whitelistMode: project?.whitelistMode ?? 'inherit',
          allowedTools: project?.allowedTools ?? [],
          approvalOverrides: (project?.approvalOverrides ?? {}) as ProjectApprovalOverridesMap,
          quickActions: project?.quickActions ?? [],
        },
      });

      // Watches — exposed so tabs can render derived data without subscribing themselves
      const whitelistMode = form.watch('whitelistMode');
      const systemPromptLen = (form.watch('systemPrompt') ?? '').length;

      // Queries (existing hooks; LIFT call sites verbatim from ProjectForm.tsx:99-110)
      const { data: integrations } = useIntegrations();
      const activePlatforms = (integrations ?? [])
        .filter((i) => i.status === 'active')
        .map((i) => i.platform);
      const { data: tools } = useTools();
      const { data: businessApprovals = {} } = useBusinessToolApprovals(/* args */);

      // Mutations
      const createMutation = useCreateProject();
      const updateMutation = useUpdateProject(project?.id ?? '');
      const deleteMutation = useDeleteProject(project?.id ?? '');

      const submitting = createMutation.isPending || updateMutation.isPending;

      const onSubmit = form.handleSubmit(async (values) => {
        try {
          const saved = isEdit
            ? await updateMutation.mutateAsync(values)
            : await createMutation.mutateAsync(values);
          onSaved(saved);
        } catch (err) {
          // Preserve current error mapping from ProjectForm.tsx:119-137 byte-identically
        }
      });

      const onDelete = async () => {
        if (!project) return;
        try {
          await deleteMutation.mutateAsync();
          // existing post-delete navigation
        } catch (err) { /* preserve */ }
      };

      const chatCount = project?.chatCount ?? 0; // or however the current code derives it

      return {
        form, isEdit, submitting,
        systemPromptLen, whitelistMode,
        activePlatforms, tools, businessApprovals, chatCount,
        onSubmit, onDelete,
      };
    }
    ```

    Verify the EXACT current Zod schema, default values, and submit handler in ProjectForm.tsx:46-147 — copy verbatim. Do not introduce new validation or default-value semantics.

    Apply project conventions:
    - `function` declarations (per frontend AGENTS.md)
    - React 18 hooks pattern; `useCallback` for stable refs where needed
    - Imports grouped: React → third-party (react-hook-form, zod, @tanstack/react-query) → internal (`@/hooks`, `@/types`)

    DO NOT touch ProjectForm.tsx in this task — that's task 19-11-03.

    Commit subject: `refactor(19): extract useProjectForm hook with full form state`.
  </action>
  <acceptance_criteria>
    - File `services/frontend/components/projects/useProjectForm.ts` exists
    - `rg -c '^export function useProjectForm\b' services/frontend/components/projects/useProjectForm.ts` returns 1
    - `rg -c '^export interface UseProjectFormResult\b' services/frontend/components/projects/useProjectForm.ts` returns 1
    - Zod schema lifted: `rg "z\.object\(" services/frontend/components/projects/useProjectForm.ts | wc -l` returns ≥1
    - `.refine()` preserved: `rg "\.refine\(" services/frontend/components/projects/useProjectForm.ts | wc -l` returns ≥1
    - File ≤300 LOC: `wc -l services/frontend/components/projects/useProjectForm.ts | awk '{exit ($1<=300)?0:1}'`
    - `cd services/frontend && pnpm typecheck` exits 0 (the new hook compiles)
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm typecheck</automated>
</task>

<task type="auto">
  <id>19-11-02</id>
  <title>Extract 4 dumb tab components</title>
  <wave>2</wave>
  <read_first>
    - services/frontend/components/projects/ProjectForm.tsx:165-326 (the 4 tab sections)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-11" lines 1133-1168)
    - services/frontend/components/projects/useProjectForm.ts (created in 19-11-01)
  </read_first>
  <action>
    Create 4 component files. Each is a "dumb" component receiving `form: UseFormReturn<FormValues>` (and additional view-state props for `ToolsTab` and `PromptTab`). All Russian copy, FormLabel/FormDescription/placeholder strings copied verbatim.

    1. **`BasicsTab.tsx`** — lifts ProjectForm.tsx:165-199:
       ```tsx
       import { type UseFormReturn } from 'react-hook-form';
       import { FormField, FormItem, FormLabel, FormControl, FormMessage } from '@/components/ui/form';
       import { Input, Textarea, TabsContent } from '@/components/ui/...';
       import type { FormValues } from './useProjectForm';

       export function BasicsTab({ form }: { form: UseFormReturn<FormValues> }) {
         return (
           <TabsContent value="basics" className="space-y-6 pt-4">
             <FormField control={form.control} name="name" render={({ field }) => (
               <FormItem>
                 <FormLabel>Название</FormLabel>
                 <FormControl><Input placeholder="..." {...field} /></FormControl>
                 <FormMessage />
               </FormItem>
             )} />
             <FormField control={form.control} name="description" render={/* ... */} />
           </TabsContent>
         );
       }
       ```
       Copy the EXACT Russian copy and field render bodies from ProjectForm.tsx:165-199.

    2. **`PromptTab.tsx`** — lifts ProjectForm.tsx:201-234. Props:
       ```tsx
       interface PromptTabProps {
         form: UseFormReturn<FormValues>;
         systemPromptLen: number;  // for the character counter
       }
       export function PromptTab({ form, systemPromptLen }: PromptTabProps) { /* ... */ }
       ```

    3. **`ToolsTab.tsx`** — lifts ProjectForm.tsx:236-307 (the wider tab). Props:
       ```tsx
       interface ToolsTabProps {
         form: UseFormReturn<FormValues>;
         whitelistMode: FormValues['whitelistMode'];
         activePlatforms: string[];
         tools: ToolEntry[] | undefined;
         businessApprovals: Record<string, 'auto' | 'manual'>;
       }
       export function ToolsTab({ form, whitelistMode, activePlatforms, tools, businessApprovals }: ToolsTabProps) { /* ... */ }
       ```
       Reuses existing sub-components (`WhitelistRadio`, `ToolCheckboxGrid`, `ProjectApprovalOverrides`) — leave imports as-is.

    4. **`QuickActionsTab.tsx`** — lifts ProjectForm.tsx:309-326:
       ```tsx
       export function QuickActionsTab({ form }: { form: UseFormReturn<FormValues> }) { /* ... */ }
       ```
       Reuses existing `QuickActionsEditor` sub-component.

    Apply project conventions:
    - `function` declarations (not arrow consts)
    - shadcn/ui imports unchanged from current ProjectForm.tsx
    - No internal state, no callbacks beyond what `form` provides — single source of truth (D-20)

    Anti-patterns:
    - Do NOT pass individual fields (`name`, `description`) to BasicsTab. Pass the whole `form` instance.
    - Do NOT add internal `useState` to any tab. Form is single source of truth.

    Commit subject: `refactor(19): extract 4 dumb ProjectForm tab components`.
  </action>
  <acceptance_criteria>
    - All 4 tab files exist under `services/frontend/components/projects/`
    - `rg -c '^export function BasicsTab\(' services/frontend/components/projects/BasicsTab.tsx` returns 1
    - `rg -c '^export function PromptTab\(' services/frontend/components/projects/PromptTab.tsx` returns 1
    - `rg -c '^export function ToolsTab\(' services/frontend/components/projects/ToolsTab.tsx` returns 1
    - `rg -c '^export function QuickActionsTab\(' services/frontend/components/projects/QuickActionsTab.tsx` returns 1
    - No internal state in tabs: `rg 'useState\(' services/frontend/components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx | wc -l` returns 0
    - Each tab file ≤200 LOC
    - Russian copy preserved: `rg 'Название|Промпт|Инструменты|Быстрые действия' services/frontend/components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx | wc -l` returns ≥4
    - `cd services/frontend && pnpm typecheck` exits 0
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm typecheck</automated>
</task>

<task type="auto">
  <id>19-11-03</id>
  <title>Rewrite ProjectForm.tsx as ≤120 LOC shell consuming hook + 4 tabs</title>
  <wave>3</wave>
  <read_first>
    - services/frontend/components/projects/ProjectForm.tsx (current 409 LOC — keep shell + buttons + delete dialog)
    - services/frontend/components/projects/useProjectForm.ts (created in 19-11-01)
    - services/frontend/components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx (created in 19-11-02)
    - services/frontend/components/projects/__tests__/ProjectForm.test.tsx (D-16: import-path/setup updates only)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("ProjectForm.tsx (rewritten)" lines 1170-1200)
  </read_first>
  <action>
    Rewrite `services/frontend/components/projects/ProjectForm.tsx` to the shell shape (~100-120 LOC):

    ```tsx
    'use client';

    import { Form } from '@/components/ui/form';
    import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
    import { Button } from '@/components/ui/button';
    import { useProjectForm } from './useProjectForm';
    import { BasicsTab } from './BasicsTab';
    import { PromptTab } from './PromptTab';
    import { ToolsTab } from './ToolsTab';
    import { QuickActionsTab } from './QuickActionsTab';
    import { DeleteProjectDialog } from './DeleteProjectDialog';
    // ... preserve any other imports needed by the shell

    interface ProjectFormProps {
      project?: Project;
      onSaved: (saved: Project) => void;
    }

    export function ProjectForm({ project, onSaved }: ProjectFormProps) {
      const {
        form, isEdit, submitting,
        systemPromptLen, whitelistMode,
        activePlatforms, tools, businessApprovals, chatCount,
        onSubmit, onDelete,
      } = useProjectForm(project, onSaved);

      // Note: if create-flow has an alternate render path (ProjectForm.tsx:328-369),
      // KEEP that branch in the shell — preserve current UX entirely.

      return (
        <Form {...form}>
          <form onSubmit={onSubmit} className="space-y-6">
            <Tabs defaultValue="basics" className="w-full">
              <TabsList>
                <TabsTrigger value="basics">Основное</TabsTrigger>
                <TabsTrigger value="prompt">Промпт</TabsTrigger>
                <TabsTrigger value="tools">Инструменты</TabsTrigger>
                <TabsTrigger value="quick-actions">Быстрые действия</TabsTrigger>
              </TabsList>
              <BasicsTab form={form} />
              <PromptTab form={form} systemPromptLen={systemPromptLen} />
              <ToolsTab
                form={form}
                whitelistMode={whitelistMode}
                activePlatforms={activePlatforms}
                tools={tools}
                businessApprovals={businessApprovals}
              />
              <QuickActionsTab form={form} />
            </Tabs>

            {/* Action buttons — LIFT VERBATIM from ProjectForm.tsx:372-395 */}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={/* cancel */}>Отмена</Button>
              <Button type="submit" disabled={submitting}>
                {isEdit ? 'Сохранить' : 'Создать'}
              </Button>
            </div>

            {isEdit && (
              <DeleteProjectDialog
                onConfirm={onDelete}
                chatCount={chatCount}
              />
            )}
          </form>
        </Form>
      );
    }
    ```

    Specific elements:
    - Form provider, tab list with the 4 tabs (Russian copy unchanged), action buttons (Сохранить/Создать text unchanged), delete dialog.
    - The create-flow alternate render (ProjectForm.tsx:328-369) — if exists, KEEP it. Lift verbatim into either the shell render branch (`if (!isEdit) return <CreateFlow />`) or move into a separate `CreateProjectForm.tsx` if cleaner. Use Claude's discretion; do not change UX.
    - Final file ≤120 LOC.

    Update existing tests at `services/frontend/components/projects/__tests__/ProjectForm.test.tsx` (D-16 import-path-only):
    - Update imports to reference `useProjectForm`, `BasicsTab`, etc. as needed.
    - Tests that mounted the whole `<ProjectForm />` continue working unchanged (component public API unchanged).
    - Tests that mounted internal slices may need to mount the new tab component directly — assertion bodies stay byte-identical.

    Apply project conventions:
    - `function` declarations
    - shadcn/ui imports unchanged
    - Russian copy unchanged

    Anti-pattern:
    - Do NOT change tab order or labels (Основное / Промпт / Инструменты / Быстрые действия) — UX preservation per D-20.

    Commit subject: `refactor(19): make ProjectForm.tsx a thin shell over useProjectForm + 4 tabs`.
  </action>
  <acceptance_criteria>
    - `wc -l services/frontend/components/projects/ProjectForm.tsx | awk '{print $1}'` returns ≤140 (target ≤120, slack for create-flow alt render if needed)
    - ProjectForm imports the hook + 4 tabs: `rg "from './(useProjectForm|BasicsTab|PromptTab|ToolsTab|QuickActionsTab)'" services/frontend/components/projects/ProjectForm.tsx | wc -l` returns ≥5
    - 4 tabs rendered: `rg '<(BasicsTab|PromptTab|ToolsTab|QuickActionsTab)\b' services/frontend/components/projects/ProjectForm.tsx | wc -l` returns 4
    - Russian tab labels preserved: `rg 'Основное|Промпт|Инструменты|Быстрые действия' services/frontend/components/projects/ProjectForm.tsx | wc -l` returns ≥4
    - `cd services/frontend && pnpm test --run components/projects` exits 0
    - `cd services/frontend && pnpm test --run` exits 0 (full suite)
    - `cd services/frontend && pnpm lint && pnpm typecheck` exits 0
    - `bash scripts/check-loc.sh` no longer flags ProjectForm.tsx
    - Test assertions unchanged: `git diff $(git merge-base HEAD main)..HEAD -- services/frontend/components/projects/__tests__/ProjectForm.test.tsx | rg '^\+\s+expect\(' | wc -l` returns 0
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm test --run components/projects</automated>
</task>

</tasks>

## Verification

```bash
# SC-01
test "$(wc -l < services/frontend/components/projects/ProjectForm.tsx)" -le 140
wc -l services/frontend/components/projects/{useProjectForm.ts,BasicsTab.tsx,PromptTab.tsx,ToolsTab.tsx,QuickActionsTab.tsx} | awk '$2!="total" && $1>300 {print; exit 1}'

# Tabs are dumb (no internal state)
test "$(rg 'useState\(' services/frontend/components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx | wc -l)" -eq 0

# Schema single source: only in useProjectForm.ts
test "$(rg 'z\.object\(' services/frontend/components/projects/ProjectForm.tsx | wc -l)" -eq 0

# SC-02
cd services/frontend && pnpm test --run && pnpm lint && pnpm typecheck
```

## Success Criteria

- `ProjectForm.tsx` ≤140 LOC (shell only)
- `useProjectForm.ts` owns full form state, schema, mutations
- 4 dumb tab components — no internal state
- Russian copy and tab order preserved (UX-identical)
- All existing ProjectForm tests pass with import-path-only updates (SC-03 / D-16)
- `pnpm test --run && pnpm lint && pnpm typecheck` green
