// components/states/EmptyInbox.tsx — Linen empty inbox state.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Инбокс пуст" row (lines 101–110). The Inbox page itself is out of
// scope per design README v2, but the pattern is reused by chat
// list / message list surfaces — keeping the component available.
//
// Brand voice: factual, single sentence, single optional action. No
// celebration of emptiness.

'use client';

import * as React from 'react';
import { useTranslations } from 'next-intl';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { EmptyFrame } from './EmptyFrame';

export interface EmptyInboxProps {
  /** Optional secondary action — e.g. "Open archive". */
  onOpenArchive?: () => void;
}

export function EmptyInbox({ onOpenArchive }: EmptyInboxProps) {
  const tStates = useTranslations('states.emptyInbox');
  return (
    <EmptyFrame
      title={tStates('title')}
      body={tStates('body')}
      action={
        onOpenArchive ? (
          <Button variant="secondary" size="sm" onClick={onOpenArchive}>
            {tStates('openArchive')}
          </Button>
        ) : undefined
      }
    />
  );
}
