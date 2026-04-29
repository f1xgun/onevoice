'use client';

import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { AlertTriangle } from 'lucide-react';
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
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';

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
}

const statusLabels: Record<string, string> = {
  active: 'Подключено',
  inactive: 'Отключено',
  error: 'Ошибка',
  pending_cookies: 'Ожидание',
  token_expired: 'Токен истёк',
};

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
}: Props) {
  const qc = useQueryClient();
  const [refreshingID, setRefreshingID] = useState<string | null>(null);

  async function refreshTelegramLinkedGroup(i: Integration) {
    setRefreshingID(i.id);
    try {
      const { data } = await api.post<{ linked_group_status: string }>(
        '/integrations/telegram/refresh',
        { channel_id: i.externalId }
      );
      if (data.linked_group_status === 'ok') {
        toast.success('Бот в группе обсуждений — комментарии будут собираться.');
      } else {
        toast.warning('Бот всё ещё не в группе обсуждений.');
      }
      qc.invalidateQueries({ queryKey: ['integrations'] });
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        'Не удалось проверить статус';
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
                Подключено
              </Badge>
            ) : (
              <Badge tone="neutral">Не подключено</Badge>
            )}
          </div>
          <div className="mt-0.5 text-[13px] text-ink-mid">{description}</div>
        </div>
      </div>

      {/* Channels list */}
      {integrations.length > 0 && (
        <div className="px-5 pb-5">
          <MonoLabel>Каналы</MonoLabel>
          <div className="mt-2.5">
            {integrations.length > 3 ? (
              <ScrollArea className="max-h-44">
                <ChannelList
                  integrations={integrations}
                  platform={platform}
                  onDisconnect={onDisconnect}
                  refreshingID={refreshingID}
                  onRefreshTelegram={refreshTelegramLinkedGroup}
                />
              </ScrollArea>
            ) : (
              <ChannelList
                integrations={integrations}
                platform={platform}
                onDisconnect={onDisconnect}
                refreshingID={refreshingID}
                onRefreshTelegram={refreshTelegramLinkedGroup}
              />
            )}
            <Button variant="secondary" size="sm" className="mt-3" onClick={onConnect}>
              + Добавить канал
            </Button>
          </div>
        </div>
      )}

      {integrations.length === 0 && (
        <div className="px-5 pb-5">
          <Button variant="primary" size="sm" onClick={onConnect}>
            Подключить
          </Button>
        </div>
      )}
    </div>
  );
}

function ChannelList({
  integrations,
  platform,
  onDisconnect,
  refreshingID,
  onRefreshTelegram,
}: {
  integrations: Integration[];
  platform: string;
  onDisconnect: (integrationId: string) => void;
  refreshingID: string | null;
  onRefreshTelegram: (i: Integration) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      {integrations.map((i) => {
        const tone = statusTones[i.status] ?? 'neutral';
        const label = statusLabels[i.status] ?? i.status;
        const channelTitle = (i.metadata as Record<string, string>)?.channel_title ?? i.externalId;
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
            <span className="min-w-0 flex-1 truncate font-mono text-[13px] text-ink">
              {channelTitle}
            </span>

            <div className="flex shrink-0 items-center gap-1.5">
              {showLinkedGroupWarn && (
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <button
                      type="button"
                      aria-label="Бот не в группе обсуждений"
                      title="Бот не в группе обсуждений — комментарии не собираются"
                      className="flex h-6 w-6 items-center justify-center rounded-full bg-warning-soft text-[var(--ov-warning-ink)] hover:bg-warning"
                    >
                      <AlertTriangle size={12} />
                    </button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Добавьте бота в группу обсуждений</AlertDialogTitle>
                      <AlertDialogDescription>
                        У этого канала есть связанная группа для комментариев, но бот в неё не
                        добавлен — поэтому комментарии к постам не собираются. Откройте группу
                        обсуждений → участники → пригласите бота канала. После этого отключите и
                        подключите канал заново, чтобы обновить статус.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Закрыть</AlertDialogCancel>
                      <AlertDialogAction
                        disabled={refreshingID === i.id}
                        onClick={(e) => {
                          e.preventDefault();
                          onRefreshTelegram(i);
                        }}
                      >
                        {refreshingID === i.id ? 'Проверяем…' : 'Я добавил бота — проверить'}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              )}

              <Badge tone={tone}>{label}</Badge>

              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button variant="ghost" size="sm" className="h-7 px-2 text-[var(--ov-danger)]">
                    Отключить
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>{`Отключить ${label}?`}</AlertDialogTitle>
                    <AlertDialogDescription>
                      История сообщений останется в архиве. Чтобы снова получать сообщения из{' '}
                      {label}, канал нужно будет подключить заново.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Отмена</AlertDialogCancel>
                    <AlertDialogAction
                      className="hover:bg-[var(--ov-danger)]/90 border-[var(--ov-danger)] bg-[var(--ov-danger)] text-[oklch(0.99_0_0)]"
                      onClick={() => onDisconnect(i.id)}
                    >
                      Отключить
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          </div>
        );
      })}
    </div>
  );
}
