'use client';

import Link from 'next/link';
import { format, parseISO } from 'date-fns';
import { ru } from 'date-fns/locale';
import type { ReactNode } from 'react';
import { ProjectChip } from '@/components/chat/ProjectChip';
import type { SearchResult } from '@/types/search';

// Phase 19 / Plan 19-04 / D-07 — search result row.
//
// Layout per CONTEXT.md / D-07:
//   [title — flex-1 truncate] [<ProjectChip size="xs"> when projectId != null] [date]
//   [snippet truncated, with backend-supplied <mark> ranges] [+N совпадений badge if matchCount>1]
//
// Click → /chat/{conversationId}?highlight={topMessageId} (the hook in
// app/(app)/chat/[id]/page.tsx scrolls to + flashes the matched message
// per D-08).
//
// The chat-row Link and the ProjectChip Link are SIBLINGS (not nested) to
// avoid the React `<a> in <a>` hydration warning — same pattern as
// PinnedSection (Plan 19-02). This means the row visually looks unified but
// the chip is a separate keyboard target navigating to /projects/{id}.

/**
 * Splits the snippet at backend-supplied byte ranges and wraps matches in <mark>.
 * Marks are byte offsets from Go (UTF-8). For Russian text in the BMP, byte
 * offsets and JS string offsets coincide; for content outside BMP this is
 * documented as a v1.4 follow-up (PATTERNS §20 gotcha).
 *
 * Pure function (no hooks); testable in isolation.
 */
export function renderHighlightedSnippet(
  snippet: string,
  marks?: Array<[number, number]>
): ReactNode {
  if (!marks || marks.length === 0) return snippet;
  const enc = new TextEncoder();
  const byteToCharIdx = (byteIdx: number): number => {
    let chars = 0;
    let bytes = 0;
    for (const ch of snippet) {
      if (bytes >= byteIdx) return chars;
      bytes += enc.encode(ch).length;
      chars += ch.length; // 1 for BMP, 2 for surrogate pairs
    }
    return chars;
  };
  const out: ReactNode[] = [];
  let cursor = 0;
  let runningKey = 0;
  for (const [start, end] of marks) {
    const cs = byteToCharIdx(start);
    const ce = byteToCharIdx(end);
    if (cs > cursor) {
      out.push(snippet.slice(cursor, cs));
    }
    out.push(
      <mark key={`m-${runningKey++}`} className="bg-yellow-200/40 text-inherit">
        {snippet.slice(cs, ce)}
      </mark>
    );
    cursor = ce;
  }
  if (cursor < snippet.length) {
    out.push(snippet.slice(cursor));
  }
  return out;
}

interface Props {
  result: SearchResult;
  /** Forwarded for the «Ничего не найдено по «{query}»» empty state — not rendered here. */
  query?: string;
  onSelect?: () => void;
}

export function SearchResultRow({ result, onSelect }: Props) {
  const href = result.topMessageId
    ? `/chat/${result.conversationId}?highlight=${encodeURIComponent(result.topMessageId)}`
    : `/chat/${result.conversationId}`;
  const dateLabel = result.lastMessageAt
    ? format(parseISO(result.lastMessageAt), 'd MMM', { locale: ru })
    : '';

  // Phase 19 / Plan 19-05 — the parent Popover.Content carries role="listbox"
  // when results.length > 0; ARIA requires its direct children to be option.
  return (
    <div role="option" aria-selected={false} className="rounded-md hover:bg-gray-800">
      <div className="flex items-center gap-2 px-2 pt-1.5">
        <Link
          href={href}
          onClick={onSelect}
          // Phase 19 / Plan 19-05 will install useRovingTabIndex; this attribute
          // is the contract anchor.
          data-roving-item="true"
          className="flex flex-1 items-center gap-2 truncate text-sm text-gray-200"
        >
          <span className="flex-1 truncate">{result.title || 'Новый диалог'}</span>
        </Link>
        {result.projectId && <ProjectChip projectId={result.projectId} size="xs" />}
        {dateLabel && <span className="shrink-0 text-xs text-gray-500">{dateLabel}</span>}
      </div>
      {result.snippet && (
        <Link
          href={href}
          onClick={onSelect}
          tabIndex={-1}
          aria-hidden="true"
          className="mt-0.5 block truncate px-2 text-xs text-gray-400"
        >
          {renderHighlightedSnippet(result.snippet, result.marks)}
        </Link>
      )}
      {result.matchCount > 1 && (
        <span className="mb-1 ml-2 mt-0.5 inline-block text-[10px] text-gray-500">
          +{result.matchCount - 1} совпадений
        </span>
      )}
      {!result.snippet && <div className="pb-1.5" />}
    </div>
  );
}
