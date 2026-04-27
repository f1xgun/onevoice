'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useConversationsQuery } from '@/hooks/useConversations';
import { useProjectsQuery } from '@/hooks/useProjects';
import { UnassignedBucket } from '@/components/sidebar/UnassignedBucket';
import { ProjectSection } from '@/components/sidebar/ProjectSection';
import type { Conversation } from '@/lib/conversations';

interface ProjectPaneProps {
  onNavigate?: () => void;
}

// ProjectPane (D-14): route-conditional column. Rendered only when the
// surrounding layout decides — i.e., on /chat/* and /projects/*. The pane
// hosts the search slot (filled by 19-04), the pinned slot (filled by
// 19-02), the «Без проекта» bucket, the project tree, and the «+ Новый
// проект» link.
export function ProjectPane({ onNavigate }: ProjectPaneProps = {}) {
  const pathname = usePathname();
  const { data: projects } = useProjectsQuery();
  const { data: conversations } = useConversationsQuery();

  const activeConversationId = useMemo(() => {
    if (!pathname.startsWith('/chat/')) return undefined;
    return pathname.split('/')[2];
  }, [pathname]);

  const { unassigned, byProject } = useMemo(() => {
    const convs: Conversation[] = conversations ?? [];
    const unassignedList = convs.filter((c) => c.projectId == null);
    const grouped: Record<string, Conversation[]> = {};
    for (const c of convs) {
      if (c.projectId != null) {
        (grouped[c.projectId] ??= []).push(c);
      }
    }
    return { unassigned: unassignedList, byProject: grouped };
  }, [conversations]);

  const sortedProjects = useMemo(() => {
    const list = projects ?? [];
    return [...list].sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  }, [projects]);

  return (
    <aside
      data-testid="project-pane"
      className="flex h-full flex-col gap-2 overflow-y-auto bg-gray-900 px-2 py-2 text-white"
    >
      {/* Slot for SidebarSearch (19-04) */}
      <div data-testid="sidebar-search-slot" />

      {/* Slot for PinnedSection (19-02) */}
      <div data-testid="pinned-section-slot" />

      <UnassignedBucket
        conversations={unassigned}
        activeConversationId={activeConversationId}
        onNavigate={onNavigate}
      />
      {sortedProjects.map((p) => (
        <ProjectSection
          key={p.id}
          project={p}
          conversations={byProject[p.id] ?? []}
          activeConversationId={activeConversationId}
          onNavigate={onNavigate}
        />
      ))}
      <Link
        href="/projects/new"
        onClick={onNavigate}
        className="mt-1 block px-2 py-1 text-xs text-gray-500 hover:text-white"
      >
        + Новый проект
      </Link>
    </aside>
  );
}
