'use client';

// app/(app)/getting-started/page.tsx — dedicated re-entry surface for the
// activation checklist (post-org, inside the authenticated shell). Renders the
// same GettingStartedChecklist as the chat empty-state but in non-dismissible
// mode so it stays visible for re-reading and shows a completed state when
// every step is done.

import { useTranslations } from 'next-intl';
import { PageHeader } from '@/components/ui/page-header';
import { GettingStartedChecklist } from '@/components/onboarding/GettingStartedChecklist';

export default function GettingStartedPage() {
  const t = useTranslations('gettingStarted');
  return (
    <>
      <PageHeader title={t('title')} sub={t('sub')} />
      <div className="px-4 pb-10 sm:px-12 sm:pb-16">
        <div className="max-w-2xl">
          <GettingStartedChecklist dismissible={false} />
        </div>
      </div>
    </>
  );
}
