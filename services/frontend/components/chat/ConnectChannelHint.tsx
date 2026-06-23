'use client';

// components/chat/ConnectChannelHint.tsx — first-run nudge shown in the chat
// empty-state when the active organization has no connected channel. Without a
// channel the quick-action chips fire an LLM turn with no available tools and
// silently do nothing, so a brand-new user otherwise hits a dead end. This
// points them at /integrations instead.

import Link from 'next/link';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { API_PATHS } from '@/lib/constants/apiPaths';

// integrationLike is the minimal shape the predicate needs — mirrors the
// /integrations list rows (status is "active" once a channel is connected).
interface integrationLike {
  status: string;
}

// shouldPromptConnectChannel decides whether to swap the quick-action chips for
// the connect-a-channel nudge. It returns true only when the integrations query
// has SUCCESSFULLY loaded (resolved === true; the caller passes isSuccess) and
// reports no active channel. An in-flight OR errored query (resolved === false)
// keeps the chips, so a user with channels never sees a flash of the nudge while
// loading and a transient fetch failure fails open to the prior behaviour.
export function shouldPromptConnectChannel(
  integrations: readonly integrationLike[] | undefined,
  resolved: boolean
): boolean {
  if (!resolved) return false;
  return !(integrations ?? []).some((i) => i.status === 'active');
}

export function ConnectChannelHint() {
  const tChat = useTranslations('chat.window');
  return (
    <div className="flex flex-col items-center gap-3 text-center">
      <p className="max-w-sm text-sm text-ink-soft">{tChat('connectChannelHint')}</p>
      <Button asChild variant="accent" size="md">
        <Link href={API_PATHS.INTEGRATIONS.ROOT}>{tChat('connectChannel')}</Link>
      </Button>
    </div>
  );
}
