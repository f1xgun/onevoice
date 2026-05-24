'use client';

import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { AlertTriangle } from 'lucide-react';
import { getIntegrationDisplay } from '@/lib/integrations';
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
} from '@/components/ui/alert-dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { MonoLabel } from '@/components/ui/mono-label';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { cn } from '@/lib/utils';

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
  // Phase 20 / ONB-04: render a "Preview" badge + tooltip on platforms whose
  // backend agent does not yet route every tool the LLM might attempt.
  // Today only google_business sets this (2 of N tools implemented per
  // services/agent-google-business/internal/agent/handler.go:63-71).
  isPreview?: boolean;
}

// Status label keys live under integrations.platformCard.status (RU+EN) —
// the rendered string comes from `tCard('status.<id>')` inside ChannelList
// so a locale switch retitles the badges without component-side state.
const STATUS_LABEL_KEYS = [
  'active',
  'inactive',
  'error',
  'pending_cookies',
  'token_expired',
] as const;

// Linen Badge tones — replaces the old default/secondary/destructive variants.
const statusTones: Record<string, 'success' | 'neutral' | 'danger' | 'warning'> = {
  active: 'success',
  inactive: 'neutral',
  error: 'danger',
  pending_cookies: 'warning',
  token_expired: 'danger',
};

// Two-letter mono initials for the platform mark — paper-sunken square per
// mock-integrations-v2.jsx IntegrationCard (no colored brand block).
function platformInitials(platform: string, label: string): string {
  if (platform === 'yandex_business') return 'ЯБ';
  if (platform === 'vk') return 'VK';
  if (platform === 'google_business') return 'GB';
  if (platform === 'telegram') return 'TG';
  return label.slice(0, 2).toUpperCase();
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
}: Props) {
  const tCard = useTranslations('integrations.platformCard');
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const [refreshingID, setRefreshingID] = useState<string | null>(null);

  async function refreshTelegramLinkedGroup(i: Integration) {
    if (!activeBusinessId) return;
    setRefreshingID(i.id);
    try {
      const { data } = await bizApi(activeBusinessId).post<{ linked_group_status: string }>(
        BIZ_API_PATHS.INTEGRATIONS.TELEGRAM_REFRESH,
        { channel_id: i.externalId }
      );
      if (data.linked_group_status === 'ok') {
        toast.success(tCard('linkedGroupOk'));
      } else {
        toast.warning(tCard('linkedGroupMissing'));
      }
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        tCard('linkedGroupCheckFailed');
      toast.error(msg);
    } finally {
      setRefreshingID(null);
    }
  }

  const hasActive = integrations.some((i) => i.status === 'active');
  const initials = platformInitials(platform, label);

  return (
    <div
      className={cn(
        'overflow-hidden rounded-lg border border-line bg-paper-raised',
        disabled && 'pointer-events-none opacity-50'
      )}
    >
      {/* Header */}
      <div className="flex items-start gap-4 px-5 py-5">
        <span
          aria-hidden
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-line-soft bg-paper-sunken font-mono text-[11px] text-ink-soft"
        >
          {initials}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2.5">
            <span className="text-[15px] font-semibold text-ink">{label}</span>
            {hasActive ? (
              <Badge tone="success" dot>
                {tCard('connected')}
              </Badge>
            ) : (
              <Badge tone="neutral">{tCard('notConnected')}</Badge>
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
          <div className="mt-0.5 text-[13px] text-ink-mid">{description}</div>
        </div>
      </div>

      {/* Channels list */}
      {integrations.length > 0 && (
        <div className="px-5 pb-5">
          <MonoLabel>{tCard('channels')}</MonoLabel>
          <div className="mt-2.5">
            {integrations.length > 3 ? (
              <ScrollArea className="max-h-44">
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
              </ScrollArea>
            ) : (
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
            )}
            {canConnect && (
              <Button variant="secondary" size="sm" className="mt-3" onClick={onConnect}>
                {tCard('addChannel')}
              </Button>
            )}
          </div>
        </div>
      )}

      {integrations.length === 0 && canConnect && (
        <div className="px-5 pb-5">
          <Button variant="primary" size="sm" onClick={onConnect}>
            {tCard('connect')}
          </Button>
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

  // Lazy backfill of missing friendly names. The first time we see an
  // integration without its platform-specific name field, we POST to the
  // platform's refresh-name endpoint and invalidate the integrations
  // query so the row re-renders with the resolved name. Tracked per-id
  // so we don't loop if the lookup permanently yields nothing.
  //
  //   - VK:               metadata.community_name (groups.getById, ~200ms)
  //   - yandex_business:  metadata.business_name  (agent get_info RPA, ~30s)
  useEffect(() => {
    if (!activeBusinessId || !canRefresh) return;
    integrations.forEach((i) => {
      if (refreshedRef.current.has(i.id)) return;
      const md = (i.metadata as Record<string, unknown>) ?? {};

      let endpoint: string | null = null;
      if (i.platform === 'vk' && typeof md.community_name !== 'string') {
        endpoint = BIZ_API_PATHS.INTEGRATIONS.VK_REFRESH_NAME(i.id);
      } else if (i.platform === 'yandex_business' && typeof md.business_name !== 'string') {
        endpoint = BIZ_API_PATHS.INTEGRATIONS.YANDEX_BUSINESS_REFRESH_NAME(i.id);
      }
      if (!endpoint) return;

      refreshedRef.current.add(i.id);
      const isYandex = i.platform === 'yandex_business';
      void bizApi(activeBusinessId)
        .post(endpoint)
        .then(() => {
          if (isYandex) {
            // Yandex returns 202 immediately and finishes the agent RPA in
            // the background (~25–45s). Poll the integrations list a few
            // times so the resolved name appears without a manual reload.
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
        .catch(() => {
          // Best-effort; swallow.
        });
    });
  }, [integrations, qc, activeBusinessId, canRefresh]);

  return (
    <div className="flex flex-col gap-2">
      {integrations.map((i) => {
        const tone = statusTones[i.status] ?? 'neutral';
        const statusLabel = (STATUS_LABEL_KEYS as readonly string[]).includes(i.status)
          ? tCard(`status.${i.status}`)
          : i.status;
        const display = getIntegrationDisplay(i, platformLabel);
        const showLinkedGroupWarn =
          platform === 'telegram' &&
          (i.metadata as Record<string, unknown>)?.linked_group_status === 'bot_not_member';

        return (
          <div
            key={i.id}
            className="flex items-center gap-3 rounded-md border border-line-soft bg-paper px-3.5 py-2.5"
          >
            <span
              aria-hidden
              className={cn(
                'h-2 w-2 shrink-0 rounded-full',
                i.status === 'active' && 'bg-success',
                i.status === 'inactive' && 'bg-ink-faint',
                i.status === 'error' && 'bg-danger',
                i.status === 'pending_cookies' && 'bg-warning',
                i.status === 'token_expired' && 'bg-danger'
              )}
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13px] text-ink">{display.name}</div>
              {display.identifier && (
                <div className="truncate font-mono text-[11px] text-ink-faint">
                  {display.identifier}
                </div>
              )}
            </div>

            <div className="flex shrink-0 items-center gap-1.5">
              {showLinkedGroupWarn && (
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <button
                      type="button"
                      aria-label={tCard('botMissingAria')}
                      title={tCard('botMissingTooltip')}
                      className="flex h-6 w-6 items-center justify-center rounded-full bg-warning-soft text-[var(--ov-warning-ink)] hover:bg-warning"
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

              <Badge tone={tone}>{statusLabel}</Badge>

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
                      <AlertDialogAction
                        className="hover:bg-[var(--ov-danger)]/90 border-[var(--ov-danger)] bg-[var(--ov-danger)] text-[oklch(0.99_0_0)]"
                        onClick={() => onDisconnect(i.id)}
                      >
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
