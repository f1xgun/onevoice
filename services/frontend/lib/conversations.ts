import { api } from './api';

// Phase 18 / TITLE-01 / TITLE-09: titleStatus drives placeholder fallback
// (D-09) and Regenerate-menu visibility (D-12). The shape is a union literal
// so consumers (chat/page.tsx, ChatHeader) can narrow without re-declaring.
export type TitleStatus = 'auto_pending' | 'auto' | 'manual';

export interface Conversation {
  id: string;
  userId: string;
  businessId: string;
  projectId: string | null;
  title: string;
  titleStatus?: TitleStatus;
  // Phase 19 / Plan 19-02 / D-02: PinnedAt is the SINGLE SOURCE OF TRUTH for
  // the pinned state. Backend serializes ISO timestamp under JSON key
  // `pinnedAt` (omitted when nil). Frontend treats `null` and `undefined`
  // identically (chat is unpinned).
  pinnedAt?: string | null;
  lastMessageAt?: string;
  createdAt: string;
  updatedAt: string;
}

// API default limit is 20 (services/api/internal/handler/conversation.go).
// The sidebar renders ALL chats grouped by project + an "Без проекта" bucket,
// so a 20-row cap silently truncates the list — deleting a chat then makes a
// formerly-page-2 chat take the freed slot, and the per-bucket counts appear
// frozen. Request the server-side max (100) so the sidebar reflects reality
// for typical users. Heavy users (>100 chats) will need real pagination.
export async function listConversations(): Promise<Conversation[]> {
  const { data } = await api.get<Conversation[]>('/conversations', {
    params: { limit: 100 },
  });
  return Array.isArray(data) ? data : [];
}

export async function createConversation(input: {
  title: string;
  projectId?: string | null;
}): Promise<Conversation> {
  const { data } = await api.post<Conversation>('/conversations', input);
  return data;
}

export async function moveConversation(
  id: string,
  projectId: string | null
): Promise<Conversation> {
  const { data } = await api.post<Conversation>(`/conversations/${id}/move`, { projectId });
  return data;
}

// Phase 19 / Plan 19-02 — pin / unpin a conversation.
// Both endpoints are scoped server-side by (id, business_id, user_id) per
// threat T-19-02-01; cross-tenant attempts return 404 (uniform — see threat
// T-19-02-02). Frontend just propagates the axios error.
export async function pinConversation(id: string): Promise<Conversation> {
  const { data } = await api.post<Conversation>(`/conversations/${id}/pin`);
  return data;
}

export async function unpinConversation(id: string): Promise<Conversation> {
  const { data } = await api.post<Conversation>(`/conversations/${id}/unpin`);
  return data;
}

export async function renameConversation(id: string, title: string): Promise<Conversation> {
  const { data } = await api.put<Conversation>(`/conversations/${id}`, { title });
  return data;
}

export async function regenerateConversationTitle(id: string): Promise<Conversation> {
  const { data } = await api.post<Conversation>(`/conversations/${id}/regenerate-title`);
  return data;
}

export async function deleteConversation(id: string): Promise<void> {
  await api.delete(`/conversations/${id}`);
}
