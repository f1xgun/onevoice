import { api } from './api';
import type { CreateProjectInput, Project, UpdateProjectInput } from '@/types/project';

export async function listProjects(): Promise<Project[]> {
  const { data } = await api.get<Project[]>('/projects');
  return Array.isArray(data) ? data : [];
}

export async function getProject(id: string): Promise<Project> {
  const { data } = await api.get<Project>(`/projects/${id}`);
  return data;
}

export async function createProject(input: CreateProjectInput): Promise<Project> {
  const { data } = await api.post<Project>('/projects', input);
  return data;
}

export async function updateProject(id: string, input: UpdateProjectInput): Promise<Project> {
  const { data } = await api.put<Project>(`/projects/${id}`, input);
  return data;
}

export async function deleteProject(
  id: string
): Promise<{ deletedConversations: number; deletedMessages: number }> {
  const { data } = await api.delete<{ deletedConversations: number; deletedMessages: number }>(
    `/projects/${id}`
  );
  return data;
}

export async function getConversationCount(id: string): Promise<number> {
  const { data } = await api.get<{ count: number }>(`/projects/${id}/conversation-count`);
  return data.count;
}
