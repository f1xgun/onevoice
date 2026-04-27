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
  pinned: boolean;
  lastMessageAt?: string;
  createdAt: string;
  updatedAt: string;
}

export async function listConversations(): Promise<Conversation[]> {
  const { data } = await api.get<Conversation[]>('/conversations');
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
