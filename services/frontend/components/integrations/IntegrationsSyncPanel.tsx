'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/design-system/AppAlertDialog';
import { Badge } from '@/components/ui/badge';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Skeleton } from '@/components/ui/skeleton';
import type { IntegrationDrift } from '@/lib/api/integrationsSync';
import { useIntegrationsDrift, useVerifyIntegrations } from '@/lib/hooks/useIntegrationsDrift';
import { usePermission } from '@/lib/hooks/usePermission';
import { getIntegrationDisplay } from '@/lib/integrations';
import { usePlatformMeta, type PlatformId } from '@/lib/platforms';
import { trackClick } from '@/lib/telemetry';

interface Integration {
  id: string;
  platform: string;
  externalId: string;
  metadata?: Record<string, unknown>;
}

interface Props {
  businessId: string;
  integrations: Integration[];
}

type SyncStatus = 'in_sync' | 'out_of_sync' | 'not_checked';

// Synced fields OneVoice pushes (platform.Field* constants). Anything outside
// this set falls back to its raw name so a future field still renders.
const KNOWN_DRIFT_FIELDS = new Set(['title', 'description', 'website', 'schedule']);

function driftKey(platform: string, externalId: string): string {
  return `${platform}::${externalId}`;
}

// syncStatusFor derives the neutral/green/amber status. A channel is only
// "in sync" once the reconciler has actually compared it (lastCheckedAt set):
// a pending row with driftDetected=false and no lastCheckedAt is "not checked",
// so we never render a false green guarantee before the first check.
export function syncStatusFor(entry: IntegrationDrift | undefined): SyncStatus {
  if (!entry || !entry.lastCheckedAt) return 'not_checked';
  return entry.driftDetected ? 'out_of_sync' : 'in_sync';
}

export function IntegrationsSyncPanel({ businessId, integrations }: Props) {
  const t = useTranslations('integrations.sync');
  const platformMeta = usePlatformMeta();
  const canVerify = usePermission('business.update').allowed;
  const { data: drift, isLoading, isSuccess, isError } = useIntegrationsDrift(businessId);
  const verifyMutation = useVerifyIntegrations(businessId);

  const driftByChannel = useMemo(() => {
    const map = new Map<string, IntegrationDrift>();
    (drift ?? []).forEach((d) => map.set(driftKey(d.platform, d.externalId), d));
    return map;
  }, [drift]);

  if (integrations.length === 0) return null;

  const fieldsText = (fields: string[]): string =>
    fields.map((f) => (KNOWN_DRIFT_FIELDS.has(f) ? t(`fields.${f}`) : f)).join(', ');

  const renderBadge = (status: SyncStatus) => {
    if (status === 'in_sync')
      return (
        <Badge tone="success" dot>
          {t('statusInSync')}
        </Badge>
      );
    if (status === 'out_of_sync')
      return (
        <Badge tone="warning" dot>
          {t('statusOutOfSync')}
        </Badge>
      );
    return <Badge tone="neutral">{t('statusNotChecked')}</Badge>;
  };

  const handleVerify = () => {
    trackClick('verify_integrations_sync');
    verifyMutation.mutate(undefined, {
      onSuccess: () => toast.success(t('started')),
      onError: () => toast.error(t('failed')),
    });
  };

  return (
    <div className="mt-12 rounded-lg border border-line bg-paper-raised p-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="text-base font-medium text-ink">{t('sectionTitle')}</div>
          <div className="mt-1 text-sm text-ink-mid">{t('sectionSubtitle')}</div>
        </div>
        {canVerify && (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button
                variant="secondary"
                size="md"
                className="shrink-0"
                disabled={verifyMutation.isPending}
              >
                {verifyMutation.isPending ? t('verifying') : t('verify')}
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{t('confirmTitle')}</AlertDialogTitle>
                <AlertDialogDescription>{t('confirmBody')}</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                <AlertDialogAction onClick={handleVerify}>{t('confirmAction')}</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </div>

      <div className="mt-5">
        {isError ? (
          <p className="text-sm text-ink-soft">{t('loadFailed')}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {integrations.map((i) => {
              const meta = platformMeta[i.platform as PlatformId];
              const platformLabel = meta?.fullLabel ?? i.platform;
              const display = getIntegrationDisplay(i, platformLabel);
              const entry = driftByChannel.get(driftKey(i.platform, i.externalId));
              const status = syncStatusFor(entry);
              return (
                <li
                  key={i.id}
                  className="flex items-center gap-3 rounded-md border border-line-soft bg-paper px-3.5 py-2.5"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[13px]">
                      <span className="text-ink-soft">{platformLabel}</span>
                      <span className="text-ink-soft"> · </span>
                      <span className="text-ink">{display.name}</span>
                    </div>
                    {isSuccess &&
                      status === 'out_of_sync' &&
                      entry &&
                      entry.driftFields.length > 0 && (
                        <div className="mt-0.5 text-[12px] text-ink-mid">
                          {t('outOfSyncHint', { fields: fieldsText(entry.driftFields) })}
                        </div>
                      )}
                    {isSuccess && status === 'not_checked' && (
                      <div className="mt-0.5 text-[12px] text-ink-faint">{t('notCheckedHint')}</div>
                    )}
                  </div>
                  <div className="shrink-0">
                    {isLoading ? (
                      <Skeleton className="h-[22px] w-28 rounded-full" />
                    ) : (
                      renderBadge(isSuccess ? status : 'not_checked')
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
