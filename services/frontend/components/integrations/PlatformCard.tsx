import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
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

  return (
    <Card className={disabled ? 'pointer-events-none opacity-40' : ''}>
      <CardContent className="space-y-3 p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div
              className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-white"
              style={{ backgroundColor: color }}
            >
              <PlatformIcon platform={platform} className="h-5 w-5" />
            </div>
            <div>
              <p className="font-medium">{label}</p>
              <p className="text-xs text-gray-500">{description}</p>
            </div>
          </div>
          {hasActive && <Badge variant="default">Подключено</Badge>}
          {!hasActive && integrations.length === 0 && <Badge variant="secondary">Отключено</Badge>}
        </div>

        {integrations.length > 0 && (
          <div className="space-y-2 border-t pt-2">
            {integrations.map((i) => (
              <div key={i.id} className="flex items-center justify-between text-sm">
                <div className="flex items-center gap-2">
                  <Badge variant={statusVariants[i.status] ?? 'secondary'} className="text-xs">
                    {statusLabels[i.status] ?? i.status}
                  </Badge>
                  <span className="text-xs text-gray-600">
                    {(i.metadata as Record<string, string>)?.channel_title ?? i.externalId}
                  </span>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onDisconnect(i.id)}
                  className="h-7 px-2 text-red-500"
                >
                  Отключить
                </Button>
              </div>
            ))}
          </div>
        )}

        <div className="flex gap-2">
          <Button size="sm" onClick={onConnect}>
            {integrations.length > 0 ? 'Добавить ещё' : 'Подключить'}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
