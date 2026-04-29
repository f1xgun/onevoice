// components/states/EmptySearch.tsx — search no-results.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Поиск не нашёл совпадений" (lines 126–135). Compact frame with mono
// rendering of the original query.
//
// SidebarSearch already owns its own dropdown empty state — we only
// expose this for full-page search surfaces and as a retuned visual
// match. The dropdown variant stays inline (text-only) for compactness.

import * as React from 'react';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';
import { EmptyFrame } from './EmptyFrame';

export interface EmptySearchProps {
  /** The query the user typed — rendered in mono inline within the title. */
  query: string;
  /** Click handler for "сбросить фильтры". Hidden when not supplied. */
  onResetFilters?: () => void;
  /** Override the default body line (e.g. "Попробуйте короче или поменяйте период."). */
  body?: React.ReactNode;
}

export function EmptySearch({ query, onResetFilters, body }: EmptySearchProps) {
  return (
    <EmptyFrame
      compact
      title={
        <>
          Ничего не нашлось по запросу{' '}
          <MonoLabel tone="ink" className="ml-0.5 text-[14px] normal-case tracking-normal">
            «{query}»
          </MonoLabel>
        </>
      }
      body={body ?? 'Попробуйте короче или поменяйте период.'}
      action={
        onResetFilters ? (
          <Button variant="ghost" size="sm" onClick={onResetFilters}>
            Сбросить фильтры
          </Button>
        ) : undefined
      }
    />
  );
}
