'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as projectsApi from '@/lib/projects';
import type { CreateProjectInput, Project, UpdateProjectInput } from '@/types/project';

export const projectsQueryKey = ['projects'] as const;
export const projectQueryKey = (id: string) => ['projects', id] as const;
export const projectConversationCountKey = (id: string) =>
  ['projects', id, 'conversation-count'] as const;

export function useProjectsQuery() {
  return useQuery<Project[]>({
    queryKey: projectsQueryKey,
    queryFn: projectsApi.listProjects,
  });
}

export function useProjectQuery(id: string) {
  return useQuery<Project>({
    queryKey: projectQueryKey(id),
    queryFn: () => projectsApi.getProject(id),
    enabled: !!id,
  });
}

export function useProjectConversationCount(id: string, enabled = true) {
  return useQuery<number>({
    queryKey: projectConversationCountKey(id),
    queryFn: () => projectsApi.getConversationCount(id),
    enabled: !!id && enabled,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateProjectInput) => projectsApi.createProject(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey });
    },
  });
}

export function useUpdateProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateProjectInput) => projectsApi.updateProject(id, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey });
      void qc.invalidateQueries({ queryKey: projectQueryKey(id) });
    },
  });
}

export function useDeleteProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => projectsApi.deleteProject(id),
    onSuccess: (_data, id) => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey });
      qc.removeQueries({ queryKey: projectQueryKey(id) });
    },
  });
}
