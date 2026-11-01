'use client';

import { useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link2 } from 'lucide-react';
import { format, parseISO, formatDistanceToNow } from 'date-fns';
import { toast } from 'sonner';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { useRevokeInvitation } from '@/lib/hooks/useInvitations';
import { useMapInviteError } from '@/lib/resolveErrorMap';
import type { PendingInvitation } from '@/lib/schemas';
import { useRoleLabel } from '@/lib/hooks/useRoleLabel';
import { RequirePermission } from '@/components/permission/RequirePermission';

import { ConfirmDestructive } from '@/components/design-system/ConfirmDestructive';

interface InvitationsTabProps {
  businessId: string;
  invitations: PendingInvitation[];
  sessionTokens: Record<string, string>;
}

export function InvitationsTab({ businessId, invitations, sessionTokens }: InvitationsTabProps) {
  const tTeam = useTranslations('team');
  const getRoleLabel = useRoleLabel();
  const tActions = useTranslations('team.invitations.actions');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const mapInviteError = useMapInviteError();
  const revoke = useRevokeInvitation(businessId);
  const [confirmRevoke, setConfirmRevoke] = useState<PendingInvitation | null>(null);

  const handleCopyLink = async (inv: PendingInvitation) => {
    const rawToken = sessionTokens[inv.id];
    if (!rawToken) return;
    const url = `${window.location.origin}/invite/${rawToken}`;
    try {
      await navigator.clipboard.writeText(url);
      toast.success(tTeam('invite.copy.toastSuccess'));
    } catch {
      toast.error(tTeam('invite.copy.toastError'));
    }
  };

  const handleRevoke = async (inv: PendingInvitation) => {
    try {
      await revoke.mutateAsync(inv.id);
      toast.success(tTeam('invitations.revoke.toastSuccess'));
    } catch (err) {
      toast.error(mapInviteError(err));
    } finally {
      setConfirmRevoke(null);
    }
  };

  if (invitations.length === 0) {
    return (
      <section className="rounded-block bg-block">
        <div className="p-block text-center md:p-block-desktop">
          <h3 className="text-base font-medium text-ink">{tTeam('invitations.empty.title')}</h3>
          <p className="mt-1 text-sm text-ink-mid">{tTeam('invitations.empty.body')}</p>
        </div>
      </section>
    );
  }

  return (
    <TooltipProvider>
      <ul className="rounded-block bg-block p-1 md:p-2">
        {invitations.map((inv) => {
          const canCopy = !!sessionTokens[inv.id];
          const expires = parseISO(inv.expires_at);
          return (
            <li key={inv.id} className="flex flex-col gap-3 p-3">
              <div className="flex items-start gap-3">
                <span
                  aria-hidden
                  className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full border border-dashed border-field text-avatar-ink"
                >
                  @
                </span>
                <div className="min-w-0 flex-1">
                  <p className="font-semibold">{getRoleLabel(inv.role_name)}</p>
                  <p
                    className="text-meta text-ink-soft"
                    title={format(expires, 'd MMMM yyyy, HH:mm', { locale: dateFnsLocale })}
                  >
                    {tTeam('invitations.expiry.relative', {
                      duration: formatDistanceToNow(expires, { locale: dateFnsLocale }),
                    })}
                  </p>
                  <p className="break-words text-meta text-ink-soft">{inv.created_by.email}</p>
                </div>
              </div>
              <div className="inline-flex items-center gap-2">
                {canCopy ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => void handleCopyLink(inv)}
                    aria-label={tActions('copyLink')}
                  >
                    <Link2 size={14} className="mr-1" aria-hidden />
                    {tActions('copyLink')}
                  </Button>
                ) : (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span tabIndex={-1}>
                        <Button variant="ghost" size="sm" disabled>
                          <Link2 size={14} className="mr-1" aria-hidden />
                          {tActions('copyLink')}
                        </Button>
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{tTeam('invitations.linkUnavailable')}</TooltipContent>
                  </Tooltip>
                )}
                <RequirePermission perm="members.invite">
                  <Button variant="danger" size="sm" onClick={() => setConfirmRevoke(inv)}>
                    {tActions('revoke')}
                  </Button>
                </RequirePermission>
              </div>
            </li>
          );
        })}
      </ul>

      {confirmRevoke && (
        <ConfirmDestructive
          open
          onOpenChange={() => setConfirmRevoke(null)}
          title={tTeam('invitations.revoke.title')}
          body={tTeam('invitations.revoke.body')}
          confirmLabel={tTeam('invitations.revoke.confirm')}
          onConfirm={() => handleRevoke(confirmRevoke)}
        />
      )}
    </TooltipProvider>
  );
}
