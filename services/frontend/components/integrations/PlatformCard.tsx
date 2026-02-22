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
import { Card, CardContent } from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import { PlatformIcon } from '@/components/integrations/PlatformIcons';

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
  color: string;
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

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive'> = {
  active: 'default',
  inactive: 'secondary',
  error: 'destructive',
  pending_cookies: 'secondary',
  token_expired: 'destructive',
};

const statusColors: Record<string, string> = {
  active: 'bg-green-500',
  inactive: 'bg-gray-400',
  error: 'bg-red-500',
  pending_cookies: 'bg-yellow-500',
  token_expired: 'bg-red-500',
};

export function PlatformCard({
  platform,
  label,
  description,
  color,
  integrations,
  onConnect,
  onDisconnect,
  disabled,
}: Props) {
  const hasActive = integrations.some((i) => i.status === 'active');
  const channelList = (
    <div className="space-y-2">
      {integrations.map((i) => (
        <div
          key={i.id}
          className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2"
        >
          <div className="flex min-w-0 items-center gap-2">
            <span
              className={`h-2 w-2 shrink-0 rounded-full ${statusColors[i.status] ?? 'bg-gray-400'}`}
            />
            <span className="min-w-0 truncate text-sm">
              {(i.metadata as Record<string, string>)?.channel_title ?? i.externalId}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Badge variant={statusVariants[i.status] ?? 'secondary'} className="text-xs">
              {statusLabels[i.status] ?? i.status}
            </Badge>
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-destructive hover:text-destructive"
                >
                  Отключить
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Отключить канал?</AlertDialogTitle>
                  <AlertDialogDescription>
                    Канал будет отключён от OneVoice. Вы сможете подключить его снова.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Отмена</AlertDialogCancel>
                  <AlertDialogAction onClick={() => onDisconnect(i.id)}>
                    Отключить
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </div>
      ))}
    </div>
  );

  return (
    <Card className={disabled ? 'pointer-events-none opacity-40' : ''}>
      <CardContent className="space-y-4 p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div
              className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg text-white"
              style={{ backgroundColor: color }}
            >
              <PlatformIcon platform={platform} className="h-6 w-6" />
            </div>
            <div>
              <p className="font-medium">{label}</p>
              <p className="text-xs text-muted-foreground">{description}</p>
            </div>
          </div>
          {hasActive && <Badge variant="default">Подключено</Badge>}
          {!hasActive && integrations.length === 0 && <Badge variant="secondary">Отключено</Badge>}
        </div>

        {integrations.length > 0 && (
          <div className="border-t pt-3">
            <p className="mb-2 text-xs font-medium text-muted-foreground">Каналы</p>
            {integrations.length > 3 ? (
              <ScrollArea className="max-h-40">{channelList}</ScrollArea>
            ) : (
              channelList
            )}
          </div>
        )}

        <Button size="sm" onClick={onConnect}>
          {integrations.length > 0 ? 'Добавить канал' : 'Подключить'}
        </Button>
      </CardContent>
    </Card>
  );
}
