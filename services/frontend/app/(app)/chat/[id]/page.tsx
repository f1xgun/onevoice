'use client';

import { use, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { useHighlightMessage } from '@/hooks/useHighlightMessage';

// Phase 19 / Plan 19-04 / D-08 — when navigated here from the search dropdown
// with `?highlight={msgId}`, scroll the matched message into view and flash it
// for ~1.75 s. The hook reads the param via next/navigation and queries the
// DOM via `[data-message-id]` (rendered by MessageBubble).
//
// Plan deviation (Rule 3): the plan assumed `useChat` was called in this file
// so it could pass `!isLoading && messages.length > 0` as the readiness flag.
// In reality, `useChat` is encapsulated inside `<ChatWindow>` (services/frontend/
// components/chat/ChatWindow.tsx). To preserve the encapsulation while still
// firing the hook from the route owner, we poll for the target element and
// flip a `messagesReady` flag once it appears (or 3 s elapses, whichever
// comes first). The hook itself silently bails when the target is missing
// (T-19-04-01 mitigation), so over-firing is safe.
function useMessagesReadyWhenHighlightTargetMounts(targetId: string | null): boolean {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    if (!targetId) {
      setReady(false);
      return;
    }
    setReady(false);

    let cancelled = false;
    const start = Date.now();
    const TIMEOUT_MS = 3000;
    const POLL_MS = 100;

    const poll = () => {
      if (cancelled) return;
      // CSS.escape mirrors the hook's selector defense.
      const el = document.querySelector(`[data-message-id="${CSS.escape(targetId)}"]`);
      if (el) {
        setReady(true);
        return;
      }
      if (Date.now() - start > TIMEOUT_MS) return;
      window.setTimeout(poll, POLL_MS);
    };
    poll();

    return () => {
      cancelled = true;
    };
  }, [targetId]);

  return ready;
}

export default function ConversationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const searchParams = useSearchParams();
  const highlightTarget = searchParams.get('highlight');
  const ready = useMessagesReadyWhenHighlightTargetMounts(highlightTarget);
  useHighlightMessage(ready);

  return <ChatWindow conversationId={id} />;
}
