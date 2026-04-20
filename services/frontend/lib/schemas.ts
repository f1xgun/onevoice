import { z } from 'zod';

export const loginSchema = z.object({
  email: z.string().email('Некорректный email').max(254),
  password: z.string().min(6, 'Минимум 6 символов'),
});

export const registerSchema = z
  .object({
    name: z.string().min(2, 'Минимум 2 символа').max(100, 'Максимум 100 символов'),
    email: z.string().email('Некорректный email').max(254),
    password: z.string().min(6, 'Минимум 6 символов'),
    confirmPassword: z.string(),
  })
  .refine((d) => d.password === d.confirmPassword, {
    message: 'Пароли не совпадают',
    path: ['confirmPassword'],
  });

export type LoginInput = z.infer<typeof loginSchema>;
export type RegisterInput = z.infer<typeof registerSchema>;

export const businessSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа').max(200, 'Максимум 200 символов'),
  category: z.string().min(1, 'Выберите категорию'),
  phone: z
    .string()
    .regex(/^\+?[0-9]{7,15}$/, 'Некорректный номер телефона')
    .optional()
    .or(z.literal('')),
  website: z.string().url('Некорректный URL').optional().or(z.literal('')),
  description: z.string().max(500).optional(),
  address: z.string().max(500).optional(),
});

export type BusinessInput = z.infer<typeof businessSchema>;

// Phase 16 — HITL tool registry & approvals.
//
// GET /api/v1/tools returns [{name, platform, floor, editableFields, description}].
// GET /api/v1/business/{id}/tool-approvals returns {toolApprovals: {[name]: "auto"|"manual"}}.
// PUT /api/v1/projects/{id} accepts {approvalOverrides: {[name]: "auto"|"manual"}} where
// inherit is encoded as KEY ABSENCE (no "inherit" string).
export const toolFloorSchema = z.enum(['auto', 'manual', 'forbidden']);
export type ToolFloor = z.infer<typeof toolFloorSchema>;

export const toolSchema = z.object({
  name: z.string(),
  displayName: z.string().default(''),
  platform: z.string(),
  floor: toolFloorSchema,
  editableFields: z.array(z.string()).default([]),
  // `description` is the LLM-facing text (may reference other tool names and
  // disambiguation rules). Never render it directly in the UI.
  description: z.string().default(''),
  // `userDescription` is the short end-user-facing copy populated per-tool in
  // the orchestrator registry. Use in settings pages / approval cards.
  userDescription: z.string().default(''),
});
export type Tool = z.infer<typeof toolSchema>;

// toolLabel returns the human-readable label for a tool — displayName when
// registered, falling back to the technical name (e.g. `telegram__send_post`).
// Use everywhere a tool is surfaced in the UI so we never leak the underscore
// format to non-technical users.
export function toolLabel(t: Pick<Tool, 'name' | 'displayName'>): string {
  return t.displayName && t.displayName.length > 0 ? t.displayName : t.name;
}

// toolUserDescription returns the end-user-facing description for a tool —
// userDescription when populated, empty string otherwise. Callers should
// render nothing rather than falling back to description, which is LLM-facing
// and may leak tool-name references to non-technical users.
export function toolUserDescription(
  t: Pick<Tool, 'userDescription'>
): string {
  return t.userDescription ?? '';
}

// tool-approvals values accept only user-settable floors: auto|manual.
// forbidden is a registration-time property and must not flow via this API.
export const toolApprovalValueSchema = z.enum(['auto', 'manual']);
export type ToolApprovalValue = z.infer<typeof toolApprovalValueSchema>;

export const toolApprovalsSchema = z.record(z.string(), toolApprovalValueSchema);
export type ToolApprovals = z.infer<typeof toolApprovalsSchema>;

export const businessToolApprovalsResponseSchema = z.object({
  toolApprovals: toolApprovalsSchema.default({}),
});
export type BusinessToolApprovalsResponse = z.infer<typeof businessToolApprovalsResponseSchema>;
