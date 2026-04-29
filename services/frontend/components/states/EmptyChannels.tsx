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
import { Button } from '@/components/ui/button';
import { EmptyFrame } from './EmptyFrame';

export interface EmptyChannelsProps {
  onConnect: () => void;
  /** Optional secondary action — e.g. open a demo / docs. */
  onViewDemo?: () => void;
}

export function EmptyChannels({ onConnect, onViewDemo }: EmptyChannelsProps) {
  return (
    <EmptyFrame
      mark="dashed"
      title="Подключите первый канал"
      body="Telegram, ВКонтакте или Яндекс.Бизнес — на ваш выбор. Сообщения и отзывы соберутся в общий ящик."
      action={
        <>
          <Button variant="accent" size="md" onClick={onConnect}>
            Подключить канал
          </Button>
          {onViewDemo && (
            <Button variant="ghost" size="md" onClick={onViewDemo}>
              Посмотреть демо
            </Button>
          )}
        </>
      }
    />
  );
}
