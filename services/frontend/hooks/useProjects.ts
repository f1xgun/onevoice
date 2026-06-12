'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as projectsApi from '@/lib/projects';
import type { CreateProjectInput, Project, UpdateProjectInput } from '@/types/project';
import { useBusinessStore } from '@/lib/stores/business';

export const projectsQueryKey = (activeBusinessId: string | null) =>
  ['businesses', activeBusinessId, 'projects'] as const;
export const projectQueryKey = (activeBusinessId: string | null, id: string) =>
  ['businesses', activeBusinessId, 'projects', id] as const;
export const projectConversationCountKey = (activeBusinessId: string | null, id: string) =>
  ['businesses', activeBusinessId, 'projects', id, 'conversation-count'] as const;

export function useProjectsQuery() {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery<Project[]>({
    queryKey: projectsQueryKey(activeBusinessId),
    queryFn: () => projectsApi.listProjects(activeBusinessId!),
    enabled: !!activeBusinessId,
  });
}

export function useProjectQuery(id: string) {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery<Project>({
    queryKey: projectQueryKey(activeBusinessId, id),
    queryFn: () => projectsApi.getProject(activeBusinessId!, id),
    enabled: !!id && !!activeBusinessId,
  });
}

export function useProjectConversationCount(id: string, enabled = true) {
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useQuery<number>({
    queryKey: projectConversationCountKey(activeBusinessId, id),
    queryFn: () => projectsApi.getConversationCount(activeBusinessId!, id),
    enabled: !!id && !!activeBusinessId && enabled,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation({
    mutationFn: (input: CreateProjectInput) => projectsApi.createProject(activeBusinessId!, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId) });
    },
  });
}

export function useUpdateProject(id: string) {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation({
    mutationFn: (input: UpdateProjectInput) =>
      projectsApi.updateProject(activeBusinessId!, id, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId) });
      void qc.invalidateQueries({ queryKey: projectQueryKey(activeBusinessId, id) });
    },
  });
}

// The projects invalidation is `exact` on purpose, and the detail cache is NOT
// removed here. The bare projects key is a prefix of the detail query
// (['businesses', id, 'projects', projectId] in useProjectQuery, still mounted
// on /projects/[id]); a non-exact invalidate prefix-matched it, and a
// removeQueries force-refetched it — both hit /projects/{id} for the
// just-deleted id → transient 404 before useProjectForm navigates away. The
// deleted project's detail cache is left to garbage-collect after unmount.
export function useDeleteProject() {
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation({
    mutationFn: (id: string) => projectsApi.deleteProject(activeBusinessId!, id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectsQueryKey(activeBusinessId), exact: true });
    },
  });
}
