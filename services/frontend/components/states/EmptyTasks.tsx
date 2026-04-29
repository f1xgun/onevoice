// components/states/EmptyTasks.tsx — empty state for /tasks.
//
// Mock anchor: design_handoff_onevoice 2/mocks/mock-states.jsx
// "Все задачи закрыты" row (lines 148–156). Hint in the mock reads
// "Не «Поздравляем!». Просто факт." — the brand voice forbids
// celebration; we just state what is.

import * as React from 'react';
import { EmptyFrame } from './EmptyFrame';

export function EmptyTasks() {
  return (
    <EmptyFrame
      title="Сегодня всё сделано"
      body="Можно выдохнуть. Если придёт что-то новое, OneVoice предупредит."
    />
  );
}
