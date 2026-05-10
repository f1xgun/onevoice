import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import type { CreateProjectInput, Project, UpdateProjectInput } from '@/types/project';

export async function listProjects(activeBusinessId: string): Promise<Project[]> {
  const { data } = await bizApi(activeBusinessId).get<Project[]>(BIZ_API_PATHS.PROJECTS.ROOT);
  return Array.isArray(data) ? data : [];
}

export async function getProject(activeBusinessId: string, id: string): Promise<Project> {
  const { data } = await bizApi(activeBusinessId).get<Project>(BIZ_API_PATHS.PROJECTS.BY_ID(id));
  return data;
}

export async function createProject(
  activeBusinessId: string,
  input: CreateProjectInput
): Promise<Project> {
  const { data } = await bizApi(activeBusinessId).post<Project>(BIZ_API_PATHS.PROJECTS.ROOT, input);
  return data;
}

export async function updateProject(
  activeBusinessId: string,
  id: string,
  input: UpdateProjectInput
): Promise<Project> {
  const { data } = await bizApi(activeBusinessId).put<Project>(
    BIZ_API_PATHS.PROJECTS.BY_ID(id),
    input
  );
  return data;
}

export async function deleteProject(
  activeBusinessId: string,
  id: string
): Promise<{ deletedConversations: number; deletedMessages: number }> {
  const { data } = await bizApi(activeBusinessId).delete<{
    deletedConversations: number;
    deletedMessages: number;
  }>(BIZ_API_PATHS.PROJECTS.BY_ID(id));
  return data;
}

export async function getConversationCount(activeBusinessId: string, id: string): Promise<number> {
  const { data } = await bizApi(activeBusinessId).get<{ count: number }>(
    BIZ_API_PATHS.PROJECTS.CONVERSATION_COUNT(id)
  );
  return data.count;
}
