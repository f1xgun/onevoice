'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link2 } from 'lucide-react';
import { format, parseISO, formatDistanceToNow } from 'date-fns';
import { ru } from 'date-fns/locale';
import { toast } from 'sonner';

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { useRevokeInvitation } from '@/lib/hooks/useInvitations';
import { useMapInviteError } from '@/lib/resolveErrorMap';
import type { PendingInvitation } from '@/lib/schemas';
import { RolePill } from '@/components/business-switcher/RolePill';
import { RequirePermission } from '@/components/permission/RequirePermission';

import { ConfirmDestructive } from './ConfirmDestructive';

interface InvitationsTabProps {
  businessId: string;
  invitations: PendingInvitation[];
  sessionTokens: Record<string, string>;
}

export function InvitationsTab({ businessId, invitations, sessionTokens }: InvitationsTabProps) {
  const tTeam = useTranslations('team');
  const tCols = useTranslations('team.invitations.cols');
  const tActions = useTranslations('team.invitations.actions');
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
      // Browser refused clipboard write (no gesture / insecure context).
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
      <section className="rounded-lg border border-line bg-paper-raised">
        <div className="px-6 py-12 text-center">
          <h3 className="text-base font-medium text-ink">{tTeam('invitations.empty.title')}</h3>
          <p className="mt-1 text-sm text-ink-mid">{tTeam('invitations.empty.body')}</p>
        </div>
      </section>
    );
  }

  return (
    <TooltipProvider>
      <section className="rounded-lg border border-line bg-paper-raised">
        <header className="border-b border-line-soft px-6 py-4">
          <p className="text-[11px] font-medium uppercase tracking-[0.04em] text-ink-soft">
            {tTeam('invitations.kicker')}
          </p>
          <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">
            {tTeam('invitations.count', { count: invitations.length })}
          </h2>
        </header>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{tCols('role')}</TableHead>
              <TableHead>{tCols('expiry')}</TableHead>
              <TableHead>{tCols('creator')}</TableHead>
              <TableHead className="text-right">{tCols('actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {invitations.map((inv) => {
              const canCopy = !!sessionTokens[inv.id];
              const expires = parseISO(inv.expires_at);
              return (
                <TableRow key={inv.id}>
                  <TableCell>
                    <RolePill roleName={inv.role_name} />
                  </TableCell>
                  <TableCell
                    className="text-sm text-ink-mid"
                    title={format(expires, 'd MMMM yyyy, HH:mm', { locale: ru })}
                  >
                    {tTeam('invitations.expiry.relative', {
                      duration: formatDistanceToNow(expires, { locale: ru }),
                    })}
                  </TableCell>
                  <TableCell className="text-sm text-ink-mid">{inv.created_by.email}</TableCell>
                  <TableCell className="text-right">
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
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </section>

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
