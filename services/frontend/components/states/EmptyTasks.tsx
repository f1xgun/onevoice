// components/states/EmptyTasks.tsx — empty state for /tasks.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Все задачи закрыты" row (lines 148–156). Hint in the mock reads
// "Не «Поздравляем!». Просто факт." — the brand voice forbids
// celebration; we just state what is.

'use client';

import * as React from 'react';
import { useTranslations } from 'next-intl';
import { EmptyFrame } from './EmptyFrame';

export function EmptyTasks() {
  const tStates = useTranslations('states.emptyTasks');
  return <EmptyFrame title={tStates('title')} body={tStates('body')} />;
}
