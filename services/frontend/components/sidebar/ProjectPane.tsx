'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useConversationsQuery } from '@/hooks/useConversations';
import { useProjectsQuery } from '@/hooks/useProjects';
import { PinnedSection } from '@/components/sidebar/PinnedSection';
import { UnassignedBucket } from '@/components/sidebar/UnassignedBucket';
import { ProjectSection } from '@/components/sidebar/ProjectSection';
import type { Conversation } from '@/lib/conversations';

interface ProjectPaneProps {
  onNavigate?: () => void;
}

// ProjectPane (D-14): route-conditional column. Rendered only when the
// surrounding layout decides — i.e., on /chat/* and /projects/*. The pane
// hosts the search slot (filled by 19-04), the PinnedSection (Phase 19 /
// Plan 19-02 / D-04+D-05 — hidden when empty), the «Без проекта» bucket,
// the project tree, and the «+ Новый проект» link.
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

  // Phase 19 / Plan 19-02 / D-03 — pinned chats sort by pinnedAt desc
  // (most-recently-pinned first). Re-pinning at the API stamps a fresh
  // now-UTC timestamp, so the sort is server-stable.
  const pinned = useMemo(() => {
    const convs: Conversation[] = conversations ?? [];
    return convs
      .filter((c) => c.pinnedAt != null)
      .sort((a, b) => (b.pinnedAt ?? '').localeCompare(a.pinnedAt ?? ''));
  }, [conversations]);

  // Lookup table for the mini ProjectChip on each pinned row (D-05).
  const projectsById = useMemo(() => {
    const out: Record<string, { id: string; name: string }> = {};
    for (const p of projects ?? []) {
      out[p.id] = { id: p.id, name: p.name };
    }
    return out;
  }, [projects]);

  return (
    <aside
      data-testid="project-pane"
      className="flex h-full flex-col gap-2 overflow-y-auto bg-gray-900 px-2 py-2 text-white"
    >
      {/* Slot for SidebarSearch (19-04) */}
      <div data-testid="sidebar-search-slot" />

      {/* Phase 19 / Plan 19-02 — PinnedSection. Hidden entirely when empty
          (D-04). The data-testid is preserved for upstream callers/tests
          that probed for the slot during Phase 19 wave-1 layout work; the
          slot is now the live component. */}
      <div data-testid="pinned-section-slot">
        <PinnedSection
          conversations={pinned}
          projectsById={projectsById}
          activeConversationId={activeConversationId}
          onNavigate={onNavigate}
        />
      </div>

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
