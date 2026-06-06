import { api } from '@/lib/api';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { API_PATHS } from '@/lib/constants/apiPaths';
import {
  invitationAcceptResponseSchema,
  invitationCreateResponseSchema,
  invitationPreviewSchema,
  pendingInvitationsListSchema,
  type InvitationAcceptResponse,
  type InvitationCreateResponse,
  type InvitationPreview,
  type PendingInvitation,
} from '@/lib/schemas';

// Backend contracts:
//   POST   /api/v1/businesses/{id}/invitations          → 200 InvitationCreateResponse
//          body: { role_id: string; expires_in?: number }
//          429 { error: "too_many_pending" }
//   GET    /api/v1/businesses/{id}/invitations          → 200 PendingInvitation[]
//   DELETE /api/v1/businesses/{id}/invitations/{id}     → 204 (no body)
//   GET    /api/v1/invitations/{token}                  → 200 InvitationPreview
//          410 { error: "gone"; reason: "expired"|"revoked"|"accepted"|"unknown" }
//   POST   /api/v1/invitations/{token}/accept           → 200 InvitationAcceptResponse
//          409 { error: "already_member" }
//          410 { error: "gone"; reason: ... }
//
// Business-scoped routes use bizApi(businessId); public token routes use
// the raw `api` instance because they live outside the `/businesses/{id}`
// hierarchy.
//
// `previewInvitation` passes `metadata: { skipBusinessNotFound: true }` so
// the 404 interceptor in lib/api.ts (which clears the active business on
// /businesses/{id} 404s) never fires for a missing token. The interceptor
// already gates on the URL prefix so this is belt-and-braces, but it makes
// the intent explicit at the call site.

export interface CreateInvitationInput {
  roleId: string;
  // Optional TTL in seconds. Backend default is 604800 (7 days);
  // valid range [3600, 2592000] (1 hour to 30 days).
  expiresIn?: number;
}

export async function createInvitation(
  businessId: string,
  input: CreateInvitationInput
): Promise<InvitationCreateResponse> {
  const body: Record<string, unknown> = { role_id: input.roleId };
  if (typeof input.expiresIn === 'number') {
    body.expires_in = input.expiresIn;
  }
  const { data } = await bizApi(businessId).post<unknown>(BIZ_API_PATHS.INVITATIONS.ROOT, body);
  return invitationCreateResponseSchema.parse(data);
}

export async function listInvitations(businessId: string): Promise<PendingInvitation[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.INVITATIONS.ROOT);
  return pendingInvitationsListSchema.parse(data);
}

export async function revokeInvitation(businessId: string, invitationId: string): Promise<void> {
  await bizApi(businessId).delete<unknown>(BIZ_API_PATHS.INVITATIONS.BY_ID(invitationId));
}

export async function previewInvitation(token: string): Promise<InvitationPreview> {
  const { data } = await api.get<unknown>(API_PATHS.INVITATIONS_PUBLIC.PREVIEW(token), {
    metadata: { skipBusinessNotFound: true },
  });
  return invitationPreviewSchema.parse(data);
}

export async function acceptInvitation(token: string): Promise<InvitationAcceptResponse> {
  const { data } = await api.post<unknown>(API_PATHS.INVITATIONS_PUBLIC.ACCEPT(token));
  return invitationAcceptResponseSchema.parse(data);
}
