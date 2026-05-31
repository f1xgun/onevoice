import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

// titleStatus drives placeholder fallback
// and Regenerate-menu visibility. The shape is a union literal
// so consumers (chat/page.tsx, ChatHeader) can narrow without re-declaring.
export type TitleStatus = 'auto_pending' | 'auto' | 'manual';

export interface Conversation {
  id: string;
  userId: string;
  businessId: string;
  projectId: string | null;
  title: string;
  titleStatus?: TitleStatus;
  // PinnedAt is the SINGLE SOURCE OF TRUTH for
  // the pinned state. Backend serializes ISO timestamp under JSON key
  // `pinnedAt` (omitted when nil). Frontend treats `null` and `undefined`
  // identically (chat is unpinned).
  pinnedAt?: string | null;
  lastMessageAt?: string;
  createdAt: string;
  updatedAt: string;
}

// API default limit is 20. The sidebar renders ALL chats grouped by project
// + an "Без проекта" bucket, so a 20-row cap silently truncates the list and
// per-bucket counts appear frozen. Request the server-side max (100) so the
// sidebar reflects reality for typical users. Heavy users (>100 chats) will
// need real pagination.
export async function listConversations(activeBusinessId: string): Promise<Conversation[]> {
  const { data } = await bizApi(activeBusinessId).get<Conversation[]>(
    BIZ_API_PATHS.CONVERSATIONS.ROOT,
    {
      params: { limit: 100 },
    }
  );
  return Array.isArray(data) ? data : [];
}

export async function createConversation(
  activeBusinessId: string,
  input: {
    title: string;
    projectId?: string | null;
  }
): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).post<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.ROOT,
    input
  );
  return data;
}

export async function moveConversation(
  activeBusinessId: string,
  id: string,
  projectId: string | null
): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).post<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.MOVE(id),
    {
      projectId,
    }
  );
  return data;
}

// pin / unpin a conversation.
// Both endpoints are scoped server-side by (id, business_id, user_id);
// cross-tenant attempts return 404 (uniform). Frontend just propagates
// the axios error.
export async function pinConversation(activeBusinessId: string, id: string): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).post<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.PIN(id)
  );
  return data;
}

export async function unpinConversation(
  activeBusinessId: string,
  id: string
): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).post<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.UNPIN(id)
  );
  return data;
}

export async function renameConversation(
  activeBusinessId: string,
  id: string,
  title: string
): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).put<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.BY_ID(id),
    {
      title,
    }
  );
  return data;
}

export async function regenerateConversationTitle(
  activeBusinessId: string,
  id: string
): Promise<Conversation> {
  const { data } = await bizApi(activeBusinessId).post<Conversation>(
    BIZ_API_PATHS.CONVERSATIONS.REGENERATE_TITLE(id)
  );
  return data;
}

export async function deleteConversation(activeBusinessId: string, id: string): Promise<void> {
  await bizApi(activeBusinessId).delete(BIZ_API_PATHS.CONVERSATIONS.BY_ID(id));
}
