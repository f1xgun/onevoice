import { z } from 'zod';

// Field length limits — kept here to keep all schema-level
// constraints in one place and to satisfy the
// `no-magic-numbers` lint rule.
const EMAIL_MAX_LEN = 254; // RFC 5321 SMTP local+domain limit
// Mirror the backend RegisterRequest constraint (validate:"min=8"). Keeping
// these in sync means a too-short password is caught inline on the form
// instead of bouncing off the API with a generic "check your details" error.
const PASSWORD_MIN_LEN = 8;
// User name (register form). Distinct from BUSINESS_NAME_MAX_LEN below
// because user names tend to be shorter than business / brand names.
const NAME_MIN_LEN = 2;
const NAME_MAX_LEN = 100;
// Business profile fields.
const BUSINESS_NAME_MAX_LEN = 200;
const BUSINESS_DESCRIPTION_MAX_LEN = 500;
const BUSINESS_ADDRESS_MAX_LEN = 500;

// Translator shape we depend on for schema messages. Compatible with both
// `useTranslations('validation')` (React) and `getServerTranslator(
// 'validation')` (async server) outputs. The `params` slot matches
// next-intl's `RichTranslationValues` shape — primitive values + Date —
// so the structural compatibility round-trips both directions.
type ValidationTranslator = (
  key: string,
  params?: Record<string, string | number | Date>
) => string;

// Request-scoped schema factories.
//
// React forms: `const t = useTranslations('validation')` and wrap with
// `useMemo(() => createXxxSchema(t), [t])` so the schema rebuilds with
// the active locale on every re-render. Server-side callers pass an
// async-resolved `t` from `getServerTranslator('validation')`.

export function createLoginSchema(t: ValidationTranslator) {
  const minChars = (count: number) => t('minChars', { count });
  return z.object({
    email: z.string().trim().toLowerCase().email(t('email')).max(EMAIL_MAX_LEN),
    password: z.string().min(PASSWORD_MIN_LEN, minChars(PASSWORD_MIN_LEN)),
  });
}

export function createRegisterSchema(t: ValidationTranslator) {
  const minChars = (count: number) => t('minChars', { count });
  const maxChars = (count: number) => t('maxChars', { count });
  const consentRequiredMessage = t('consentRequired');
  return z
    .object({
      name: z
        .string()
        .min(NAME_MIN_LEN, minChars(NAME_MIN_LEN))
        .max(NAME_MAX_LEN, maxChars(NAME_MAX_LEN)),
      email: z.string().trim().toLowerCase().email(t('email')).max(EMAIL_MAX_LEN),
      password: z.string().min(PASSWORD_MIN_LEN, minChars(PASSWORD_MIN_LEN)),
      confirmPassword: z.string(),
      acceptTosPrivacy: z.literal(true, { message: consentRequiredMessage }),
      acceptPdn: z.literal(true, { message: consentRequiredMessage }),
    })
    .refine((d) => d.password === d.confirmPassword, {
      message: t('passwordsMismatch'),
      path: ['confirmPassword'],
    });
}

// Mirror the backend WaitlistRequest constraint (validate:"max=320"). The
// sphere/pain enums are validated server-side against a closed allow-list, so
// the form keeps them as free-optional strings and only gates email + consent.
const WAITLIST_EMAIL_MAX_LEN = 320;

export function createWaitlistSchema(t: ValidationTranslator) {
  const consentRequiredMessage = t('consentRequired');
  return z.object({
    email: z.string().email(t('email')).max(WAITLIST_EMAIL_MAX_LEN),
    sphere: z.string().optional(),
    pain: z.string().optional(),
    consent: z.literal(true, { message: consentRequiredMessage }),
  });
}

export function createBusinessSchema(t: ValidationTranslator) {
  const minChars = (count: number) => t('minChars', { count });
  const maxChars = (count: number) => t('maxChars', { count });
  return z.object({
    name: z
      .string()
      .min(NAME_MIN_LEN, minChars(NAME_MIN_LEN))
      .max(BUSINESS_NAME_MAX_LEN, maxChars(BUSINESS_NAME_MAX_LEN)),
    category: z.string().min(1, t('businessCategoryRequired')),
    phone: z
      .string()
      .regex(/^\+?[0-9]{7,15}$/, t('phone'))
      .optional()
      .or(z.literal('')),
    // The API returns `website` as `null` when unset (domain.Business.Website
    // is a *string). Accept null here so loading an existing profile with no
    // website doesn't fail validation on save — same pattern as the other
    // API-nullable fields in this file.
    website: z.string().url(t('url')).optional().or(z.literal('')).nullable(),
    description: z.string().max(BUSINESS_DESCRIPTION_MAX_LEN).optional(),
    address: z.string().max(BUSINESS_ADDRESS_MAX_LEN).optional(),
  });
}

// Inferred types stay stable across the factory because the resulting Zod
// shape doesn't depend on `t`. `ReturnType<typeof createXxxSchema>` is the
// canonical way to spell that.
export type LoginInput = z.infer<ReturnType<typeof createLoginSchema>>;
export type RegisterInput = z.infer<ReturnType<typeof createRegisterSchema>>;
export type WaitlistInput = z.infer<ReturnType<typeof createWaitlistSchema>>;
export type BusinessInput = z.infer<ReturnType<typeof createBusinessSchema>>;

// HITL tool registry & approvals.
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
export function toolUserDescription(t: Pick<Tool, 'userDescription'>): string {
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

// ---------------------------------------------------------------------------
// RBAC — members, roles, invitations.
//
// Every response body from the endpoints is parsed with `.parse`
// (not `safeParse`) so malformed data throws at the API seam rather than
// crashing the UI later. See plan 04-02 threat -01.
// ---------------------------------------------------------------------------

export const memberSchema = z.object({
  user: z.object({
    id: z.string(),
    email: z.string().email(),
    name: z.string().optional(),
  }),
  role: z.object({
    id: z.string(),
    name: z.string(),
    permissions: z.array(z.string()),
  }),
  status: z.enum(['active', 'suspended']),
  joined_at: z.string(),
  invited_by: z.string().nullable(),
  invited_at: z.string().nullable(),
});
export type Member = z.infer<typeof memberSchema>;
export const membersListSchema = z.array(memberSchema);

export const roleSchema = z.object({
  id: z.string(),
  business_id: z.string().nullable(),
  name: z.string(),
  description: z.string().optional().default(''),
  permissions: z.array(z.string()),
  is_system: z.boolean(),
  // populated by the role-list endpoint (GET /businesses/{id}/roles)
  // so the role list can render a «N участников» badge without a second fetch.
  // Optional because the POST/PATCH response does NOT include it (those return
  // the role document only — list/detail differ here by design).
  member_count: z.number().int().nonnegative().optional(),
});
export type Role = z.infer<typeof roleSchema>;
export const rolesListSchema = z.array(roleSchema);

// RBAC dynamic registry — schemas for /permissions (catalog) and
// /businesses/{id}/me/permissions (effective).
//
// Catalog wire shape (services/api/internal/handler/permissions.go):
// { groups: [ { resource: string, permissions: [{name, description}] } ] }
// Effective wire shape (services/api/internal/handler/permissions.go me handler):
// { permissions: string[] }
//
// Catalog is app-static (cached with staleTime: Infinity); the effective
// list is per-actor-per-business (staleTime: 60_000 + refetchInterval: 60_000).
export const permissionMetaSchema = z.object({
  name: z.string(),
  description: z.string(),
});
export const permissionGroupSchema = z.object({
  resource: z.string(),
  permissions: z.array(permissionMetaSchema),
});
export const permissionsCatalogSchema = z.object({
  groups: z.array(permissionGroupSchema),
});
export type PermissionMeta = z.infer<typeof permissionMetaSchema>;
export type PermissionGroup = z.infer<typeof permissionGroupSchema>;
export type PermissionsCatalog = z.infer<typeof permissionsCatalogSchema>;

export const myPermissionsSchema = z.object({
  permissions: z.array(z.string()),
});
export type MyPermissions = z.infer<typeof myPermissionsSchema>;

export const pendingInvitationSchema = z.object({
  id: z.string(),
  role_id: z.string(),
  role_name: z.string(),
  expires_at: z.string(),
  created_at: z.string(),
  created_by: z.object({
    id: z.string(),
    email: z.string().email(),
  }),
});
export type PendingInvitation = z.infer<typeof pendingInvitationSchema>;
export const pendingInvitationsListSchema = z.array(pendingInvitationSchema);

export const invitationCreateResponseSchema = z.object({
  id: z.string(),
  token: z.string(),
  role_id: z.string(),
  expires_at: z.string(),
  created_at: z.string(),
});
export type InvitationCreateResponse = z.infer<typeof invitationCreateResponseSchema>;

export const invitationPreviewSchema = z.object({
  business_id: z.string(),
  business_name: z.string(),
  role_id: z.string(),
  role_name: z.string(),
  expires_at: z.string(),
});
export type InvitationPreview = z.infer<typeof invitationPreviewSchema>;

export const invitationAcceptResponseSchema = z.object({
  business_id: z.string(),
  role_id: z.string(),
});
export type InvitationAcceptResponse = z.infer<typeof invitationAcceptResponseSchema>;
