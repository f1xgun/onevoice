import { api } from './api';

export interface Conversation {
  id: string;
  userId: string;
  businessId: string;
  projectId: string | null;
  title: string;
  titleStatus: string;
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
