'use client';

// components/states/EmptyChannels.tsx — first-run state for /integrations.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Каналы не подключены" row (lines 112–124).
//
// Two actions max:
//   - Primary (accent / ochre): "Подключить канал"
//   - Ghost: "Посмотреть демо" (optional)
//
// The Linen rule allows one ochre moment per screen — this is it.
// /integrations otherwise stays graphite.

import * as React from 'react';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { EmptyFrame } from './EmptyFrame';

export interface EmptyChannelsProps {
  onConnect: () => void;
  /** Optional secondary action — e.g. open a demo / docs. */
  onViewDemo?: () => void;
}

export function EmptyChannels({ onConnect, onViewDemo }: EmptyChannelsProps) {
  const tStates = useTranslations('states.emptyChannels');
  return (
    <EmptyFrame
      mark="dashed"
      title={tStates('title')}
      body={tStates('body')}
      action={
        <>
          <Button variant="accent" size="md" onClick={onConnect}>
            {tStates('connect')}
          </Button>
          {onViewDemo && (
            <Button variant="ghost" size="md" onClick={onViewDemo}>
              {tStates('viewDemo')}
            </Button>
          )}
        </>
      }
    />
  );
}
