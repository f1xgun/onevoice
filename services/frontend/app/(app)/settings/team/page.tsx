'use client';

import { useState } from 'react';
import { LoadingPlaceholder } from '@/components/states/LoadingPlaceholder';
import { ListLoadError } from '@/components/lists/ListLoadError';
import { useTranslations } from 'next-intl';
import Link from 'next/link';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { useInvitations } from '@/lib/hooks/useInvitations';
import { useRoles } from '@/lib/hooks/useMembers';

import { MembersTab } from './_components/MembersTab';
import { InvitationsTab } from './_components/InvitationsTab';
import { InviteModal } from './_components/InviteModal';

export default function TeamPage() {
  const tPage = useTranslations('team.page');
  const tV2 = useTranslations('settings.v2');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const {
    data: invitations = [],
    isLoading,
    isError,
    isFetching,
    refetch,
  } = useInvitations(activeBusinessId);
  const { data: roles = [] } = useRoles(activeBusinessId);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [sessionTokens, setSessionTokens] = useState<Record<string, string>>({});

  const handleInvitationCreated = (invitationId: string, rawToken: string) => {
    setSessionTokens((prev) => ({ ...prev, [invitationId]: rawToken }));
  };

  if (!activeBusinessId) {
    return null;
  }

  return (
    <div className="mx-auto max-w-[816px] space-y-group px-gutter pb-10 pt-5 md:px-gutter-desktop">
      <header className="flex items-center justify-between gap-3">
        <h1 className="text-page-title">{tPage('title')}</h1>
        <RequirePermission perm="members.invite">
          <Button onClick={() => setInviteOpen(true)}>{tPage('invite')}</Button>
        </RequirePermission>
      </header>
      <section>
        <h2 className="mb-block-gap text-group-title">{tV2('inTeam')}</h2>
        <MembersTab businessId={activeBusinessId} roles={roles} />
      </section>
      <section>
        <h2 className="mb-block-gap text-group-title">{tV2('pending')}</h2>
        {isLoading ? (
          <LoadingPlaceholder className="h-48 animate-pulse rounded-lg bg-paper-sunken" />
        ) : isError ? (
          <ListLoadError onRetry={() => void refetch()} isPending={isFetching} />
        ) : (
          <InvitationsTab
            businessId={activeBusinessId}
            invitations={invitations}
            sessionTokens={sessionTokens}
          />
        )}
      </section>
      <p className="text-[15px] leading-[22px] text-ink-soft">
        {tV2('customRolesHint')}
        <br />
        <Link
          href="/settings/roles"
          className="inline-flex min-h-tap items-center text-brand underline"
        >
          {tV2('customRoles')}
        </Link>
      </p>
      <InviteModal
        open={inviteOpen}
        onOpenChange={setInviteOpen}
        businessId={activeBusinessId}
        roles={roles}
        onInvitationCreated={handleInvitationCreated}
      />
    </div>
  );
}
