'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { useConversationsQuery } from '@/hooks/useConversations';
import { useProjectsQuery } from '@/hooks/useProjects';
import { PinnedSection } from '@/components/sidebar/PinnedSection';
import { SidebarSearch } from '@/components/sidebar/SidebarSearch';
import { UnassignedBucket } from '@/components/sidebar/UnassignedBucket';
import { ProjectSection } from '@/components/sidebar/ProjectSection';
import type { Conversation } from '@/lib/conversations';

interface ProjectPaneProps {
  onNavigate?: () => void;
}

// ProjectPane: route-conditional column. Rendered only when the surrounding
// layout decides — i.e., on /chat/* and /projects/*. The pane hosts the
// search slot, the PinnedSection (hidden when empty), the «Без проекта»
// bucket, the project tree, and the «+ Новый проект» link.
export function ProjectPane({ onNavigate }: ProjectPaneProps = {}) {
  const tSide = useTranslations('sidebar');
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

  const pinned = useMemo(() => {
    const convs: Conversation[] = conversations ?? [];
    return convs
      .filter((c) => c.pinnedAt != null)
      .sort((a, b) => (b.pinnedAt ?? '').localeCompare(a.pinnedAt ?? ''));
  }, [conversations]);

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
      aria-label={tSide('paneAria')}
      className="flex h-full flex-col gap-2 overflow-y-auto bg-paper-raised px-2 py-2 text-ink"
    >
      {/* SidebarSearch. The data-testid wrapper is preserved so earlier
          tests that probed for the slot keep passing; the slot now hosts
          the live search input + Radix Popover dropdown (Cmd/Ctrl-K
          consumer, 250 ms debounce, route-aware scope). */}
      <div data-testid="sidebar-search-slot">
        <SidebarSearch />
      </div>

      {/* PinnedSection. Hidden entirely when empty. The data-testid is
          preserved for upstream callers/tests that probed for the slot
          during earlier wave-1 layout work; the slot is now the live
          component. */}
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
        className="mt-1 block px-2 py-1 text-xs text-ink-soft hover:text-ink"
      >
        {tSide('newProject')}
      </Link>
    </aside>
  );
}
