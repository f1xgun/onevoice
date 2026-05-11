'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { UserPlus } from 'lucide-react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { PageHeader } from '@/components/ui/page-header';
import { RequirePermission } from '@/components/permission/RequirePermission';
import { useBusinessStore } from '@/lib/stores/business';
import { useInvitations } from '@/lib/hooks/useInvitations';
import { useRoles } from '@/lib/hooks/useMembers';

import { MembersTab } from './_components/MembersTab';
import { InvitationsTab } from './_components/InvitationsTab';
import { InviteModal } from './_components/InviteModal';

export default function TeamPage() {
  const tPage = useTranslations('team.page');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data: invitations = [] } = useInvitations(activeBusinessId);
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
    <div className="px-4 pb-10 pt-6 sm:px-12 sm:pb-16">
      <PageHeader
        title={tPage('title')}
        sub={tPage('sub')}
        actions={
          <RequirePermission perm="members.invite">
            <Button onClick={() => setInviteOpen(true)}>
              <UserPlus size={16} className="mr-2" aria-hidden />
              {tPage('invite')}
            </Button>
          </RequirePermission>
        }
      />
      <Tabs defaultValue="members" className="mt-6">
        <TabsList
          aria-label={tPage('tabsAria')}
          className="inline-flex h-auto items-center justify-start gap-6 rounded-none border-b border-line bg-transparent p-0"
        >
          <TabsTrigger
            value="members"
            className="rounded-none border-b-2 border-transparent bg-transparent px-0 pb-2 text-ink-soft shadow-none data-[state=active]:border-ink data-[state=active]:bg-transparent data-[state=active]:text-ink data-[state=active]:shadow-none"
          >
            {tPage('tabs.members')}
          </TabsTrigger>
          <TabsTrigger
            value="invitations"
            className="rounded-none border-b-2 border-transparent bg-transparent px-0 pb-2 text-ink-soft shadow-none data-[state=active]:border-ink data-[state=active]:bg-transparent data-[state=active]:text-ink data-[state=active]:shadow-none"
          >
            <span>{tPage('tabs.invitations')}</span>
            {invitations.length > 0 && (
              <span className="ml-2 inline-flex h-5 min-w-[20px] items-center justify-center rounded-md bg-ink px-2 font-mono text-[11px] leading-none text-paper">
                {invitations.length}
              </span>
            )}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="members" className="mt-6">
          <MembersTab businessId={activeBusinessId} roles={roles} />
        </TabsContent>
        <TabsContent value="invitations" className="mt-6">
          <InvitationsTab
            businessId={activeBusinessId}
            invitations={invitations}
            sessionTokens={sessionTokens}
          />
        </TabsContent>
      </Tabs>
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
