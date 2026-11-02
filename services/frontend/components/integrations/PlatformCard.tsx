'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { AlertTriangle, ChevronRight, CheckCircle2 } from 'lucide-react';

import { PlatformIcon } from './PlatformIcons';

import { getIntegrationDisplay } from '@/lib/integrations';
import {
  PLATFORM_DISPLAY_FIELD,
  PLATFORM_REFRESH_ENDPOINTS,
  isKnownPlatform,
  type PlatformId,
} from '@/lib/platforms';
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
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { channelConnectionState, readConnectionHealth } from '@/lib/constants/integrationStatus';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { extractApiErrorCode } from '@/lib/resolveErrorMap';
import { cn } from '@/lib/utils';
import { formatClockTime } from '@/lib/time-display';

// Yandex.Business RPA refresh poll cadence (ms). After kicking off a
// refresh the agent runs for ~25–45s in the background, so we revisit
// the integrations list at progressively longer intervals to surface the
// resolved name without forcing the operator to reload manually.
const YANDEX_REFRESH_POLL_FAST_MS = 10_000;
const YANDEX_REFRESH_POLL_MEDIUM_MS = 30_000;
const YANDEX_REFRESH_POLL_SLOW_MS = 60_000;
const YANDEX_REFRESH_POLL_MS = [
  YANDEX_REFRESH_POLL_FAST_MS,
  YANDEX_REFRESH_POLL_MEDIUM_MS,
  YANDEX_REFRESH_POLL_SLOW_MS,
] as const;

interface Integration {
  id: string;
  platform: string;
  status: string;
  externalId: string;
  metadata?: Record<string, unknown>;
}

interface Props {
  platform: string;
  label: string;
  description: string;
  integrations: Integration[];
  onConnect: () => void;
  onDisconnect: (integrationId: string) => void;
  disabled?: boolean;
  canConnect?: boolean;
  canDisconnect?: boolean;
  // render a "Preview" badge + tooltip on platforms whose
  // backend agent does not yet route every tool the LLM might attempt.
  // Today only google_business sets this (2 of N tools implemented per
  // services/agent-google-business/internal/agent/handler.go:63-71).
  isPreview?: boolean;
  checkedAt?: string;
}

export function PlatformCard({
  platform,
  label,
  description,
  integrations,
  onConnect,
  onDisconnect,
  disabled,
  canConnect = true,
  canDisconnect = true,
  isPreview = false,
  checkedAt,
}: Props) {
  const tCard = useTranslations('integrations.platformCard');
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const [refreshingID, setRefreshingID] = useState<string | null>(null);

  async function refreshTelegramLinkedGroup(i: Integration) {
    if (!activeBusinessId) return;
    const telegramRefresh = INTEGRATION_ENDPOINTS.telegram?.refresh;
    if (!telegramRefresh) return;
    setRefreshingID(i.id);
    try {
      const { data } = await bizApi(activeBusinessId).post<{ linked_group_status: string }>(
        telegramRefresh,
        { channel_id: i.externalId }
      );
      if (data.linked_group_status === 'ok') {
        toast.success(tCard('linkedGroupOk'));
      } else {
        toast.warning(tCard('linkedGroupMissing'));
      }
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
    } catch (err: unknown) {
      const msg = extractApiErrorCode(err) || tCard('linkedGroupCheckFailed');
      toast.error(msg);
    } finally {
      setRefreshingID(null);
    }
  }

  const connection = channelConnectionState(integrations);

  const needsReconnect = connection === 'error';
  const header = (
    <>
      <PlatformIcon platform={platform} />
      <div className="min-w-0 flex-1">
        <h2 className="text-reading font-semibold">{label}</h2>
        {integrations.length === 0 ? (
          <p className="mt-1 text-meta text-ink-soft">{description}</p>
        ) : (
          integrations.map((i) => {
            const display = getIntegrationDisplay(i, label);
            const name =
              display.name === label
                ? ''
                : ['telegram', 'vk'].includes(platform)
                  ? `«${display.name.replace(/^«|»$/g, '')}»`
                  : display.name;
            const secondary =
              display.memberCount !== undefined
                ? tCard('subscribers', { count: display.memberCount })
                : display.identifier;
            const health = readConnectionHealth(i.metadata);
            const reason = health?.reason_code ? `health.reason.${health.reason_code}` : '';
            return (
              <div key={i.id} className="mt-1 break-words text-reading text-ink-soft">
                {(name || secondary) && <p>{[name, secondary].filter(Boolean).join(' · ')}</p>}
                {(health?.status === 'broken' || health?.status === 'degraded') &&
                  reason &&
                  tCard.has(reason) && (
                    <p className="mt-1 text-meta text-danger">{tCard(reason)}</p>
                  )}
              </div>
            );
          })
        )}
        <p
          className={cn(
            'mt-1 flex items-start gap-2 text-reading',
            needsReconnect ? 'text-danger' : 'text-ink'
          )}
        >
          {connection === 'connected' ? (
            <CheckCircle2 aria-hidden className="mt-1 h-head-sm w-head-sm shrink-0 text-ok" />
          ) : (
            <span
              aria-hidden
              className={cn(
                'mt-2 h-2.5 w-2.5 shrink-0 rounded-full',
                needsReconnect ? 'bg-danger' : 'bg-field'
              )}
            />
          )}
          {integrations.some((i) => i.status === 'token_expired')
            ? tCard('status.token_expired')
            : tCard(
                connection === 'connected'
                  ? 'working'
                  : connection === 'disconnected'
                    ? 'notConnected'
                    : `connection.${connection}`
              )}
          {connection === 'connected' && checkedAt && formatClockTime(checkedAt) && (
            <span>{tCard('checkedAt', { time: formatClockTime(checkedAt) })}</span>
          )}
        </p>
        {integrations.some((i) => readConnectionHealth(i.metadata)?.status === 'degraded') && (
          <p className="mt-1 text-meta text-warning">{tCard('health.degradedLabel')}</p>
        )}
        {isPreview && (
          <Badge
            tone="warning"
            title={tCard('previewTooltip')}
            aria-label={tCard('previewTooltip')}
          >
            {tCard('previewBadge')}
          </Badge>
        )}
      </div>
    </>
  );

  return (
    <div className={cn('p-3', disabled && 'opacity-50')}>
      {integrations.length > 0 ? (
        <details className="group">
          <summary className="flex min-h-tap cursor-pointer list-none items-start gap-3 [&::-webkit-details-marker]:hidden">
            {header}
            <ChevronRight
              aria-hidden
              className="mt-1 h-head-sm w-head-sm shrink-0 text-ink-soft group-open:rotate-90"
            />
          </summary>
          <div className="ml-11 mt-3">
            <ChannelList
              integrations={integrations}
              platform={platform}
              platformLabel={label}
              onDisconnect={onDisconnect}
              canDisconnect={canDisconnect}
              refreshingID={refreshingID}
              onRefreshTelegram={refreshTelegramLinkedGroup}
              activeBusinessId={activeBusinessId}
            />
            {canConnect && (
              <Button
                variant="secondary"
                size="sm"
                className="mt-3"
                onClick={onConnect}
                disabled={disabled}
              >
                {tCard('addChannel')}
              </Button>
            )}
          </div>
        </details>
      ) : (
        <div className="flex items-start gap-3">{header}</div>
      )}
      {canConnect && (integrations.length === 0 || needsReconnect) && (
        <div className="ml-11 mt-3">
          {needsReconnect ? (
            <Button asChild disabled={disabled}>
              <Link href={`/integrations?reconnect=${platform}`}>{tCard('reconnect')}</Link>
            </Button>
          ) : (
            <Button variant="secondary" onClick={onConnect} disabled={disabled}>
              {tCard('connect')}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

function ChannelList({
  integrations,
  platform,
  platformLabel,
  onDisconnect,
  canDisconnect,
  refreshingID,
  onRefreshTelegram,
  activeBusinessId,
}: {
  integrations: Integration[];
  platform: string;
  platformLabel: string;
  onDisconnect: (integrationId: string) => void;
  canDisconnect: boolean;
  refreshingID: string | null;
  onRefreshTelegram: (i: Integration) => void;
  activeBusinessId: string | null;
}) {
  const tCard = useTranslations('integrations.platformCard');
  const tCommon = useTranslations('common');
  const qc = useQueryClient();
  const refreshedRef = useRef<Set<string>>(new Set());
  const canRefresh = usePermission('integrations.connect').allowed;

  useEffect(() => {
    if (!activeBusinessId || !canRefresh) return;
    integrations.forEach((i) => {
      if (refreshedRef.current.has(i.id)) return;
      const md = (i.metadata as Record<string, unknown>) ?? {};

      const buildEndpoint = isKnownPlatform(i.platform)
        ? PLATFORM_REFRESH_ENDPOINTS[i.platform]
        : undefined;
      if (!buildEndpoint) return;

      const displayField = PLATFORM_DISPLAY_FIELD[i.platform as PlatformId];
      if (displayField && typeof md[displayField] === 'string') return;

      const endpoint = buildEndpoint(i.id);

      refreshedRef.current.add(i.id);
      const isYandex = i.platform === 'yandex_business';
      void bizApi(activeBusinessId)
        .post(endpoint)
        .then(() => {
          if (isYandex) {
            YANDEX_REFRESH_POLL_MS.forEach((delay) => {
              setTimeout(
                () =>
                  qc.invalidateQueries({
                    queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId),
                  }),
                delay
              );
            });
          } else {
            qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
          }
        })
        .catch(() => {});
    });
  }, [integrations, qc, activeBusinessId, canRefresh]);

  return (
    <div className="flex flex-col gap-2">
      {integrations.map((i) => {
        const connection = channelConnectionState([i]);
        const display = getIntegrationDisplay(i, platformLabel);
        const showLinkedGroupWarn =
          platform === 'telegram' &&
          (i.metadata as Record<string, unknown>)?.linked_group_status === 'bot_not_member';

        const health = readConnectionHealth(i.metadata as Record<string, unknown> | undefined);
        const alarmingHealth =
          health?.status === 'broken' || health?.status === 'degraded' ? health.status : null;
        const reasonKey = health?.reason_code ? `health.reason.${health.reason_code}` : '';
        const healthReason = reasonKey && tCard.has(reasonKey) ? tCard(reasonKey) : '';

        return (
          <div
            key={i.id}
            className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-b border-line py-4 md:grid-cols-[auto_minmax(0,1fr)_auto]"
          >
            <span
              aria-hidden
              className={cn(
                'h-2 w-2 shrink-0 rounded-full',
                connection === 'connected' && 'bg-success',
                connection === 'error' && 'bg-[var(--ov-danger)]',
                connection === 'unknown' && 'bg-ink-faint'
              )}
            />
            <div className="min-w-0 flex-1">
              <div className="break-words text-reading text-ink">{display.name}</div>
              {display.identifier && (
                <div className="break-all font-mono text-meta text-ink-faint">
                  {display.identifier}
                </div>
              )}
              {alarmingHealth && healthReason && (
                <div
                  className={cn(
                    'mt-1 text-meta',
                    alarmingHealth === 'broken'
                      ? 'text-[var(--ov-danger)]'
                      : 'text-[var(--ov-warning-ink)]'
                  )}
                >
                  {healthReason}
                </div>
              )}
            </div>

            <div className="col-span-2 flex flex-wrap items-center gap-2 md:col-span-1">
              {showLinkedGroupWarn && (
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <button
                      type="button"
                      aria-label={tCard('botMissingAria')}
                      title={tCard('botMissingTooltip')}
                      className="flex h-11 w-11 items-center justify-center rounded-md bg-paper-raised text-warning hover:bg-paper-sunken"
                    >
                      <AlertTriangle size={12} />
                    </button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>{tCard('addBotToGroupTitle')}</AlertDialogTitle>
                      <AlertDialogDescription>{tCard('addBotToGroupBody')}</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{tCard('close')}</AlertDialogCancel>
                      <AlertDialogAction
                        disabled={refreshingID === i.id}
                        onClick={(e) => {
                          e.preventDefault();
                          onRefreshTelegram(i);
                        }}
                      >
                        {refreshingID === i.id ? tCard('botRechecking') : tCard('botRecheck')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              )}

              {canDisconnect && (
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="ghost" size="sm" className="h-7 px-2 text-[var(--ov-danger)]">
                      {tCard('disconnect')}
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {tCard('disconnectTitle', { name: display.name })}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {tCard('disconnectBody', { name: display.name })}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{tCommon('cancel')}</AlertDialogCancel>
                      <AlertDialogAction variant="danger" onClick={() => onDisconnect(i.id)}>
                        {tCard('disconnect')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
