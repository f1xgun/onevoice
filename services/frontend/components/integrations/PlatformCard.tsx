import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';

interface Props {
  platform: string;
  label: string;
  description: string;
  color: string;
  status: 'active' | 'inactive' | 'error' | null;
  lastSyncAt?: string;
  onConnect: () => void;
  onDisconnect: () => void;
  disabled?: boolean;
}

const statusLabels: Record<string, string> = {
  active: 'Подключено',
  inactive: 'Отключено',
  error: 'Ошибка',
};

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive'> = {
  active: 'default',
  inactive: 'secondary',
  error: 'destructive',
};

export function PlatformCard({
  label,
  description,
  color,
  status,
  lastSyncAt,
  onConnect,
  onDisconnect,
  disabled,
}: Props) {
  const connected = status === 'active';

  return (
    <Card className={disabled ? 'pointer-events-none opacity-40' : ''}>
      <CardContent className="space-y-3 p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div
              className="flex h-10 w-10 items-center justify-center rounded-lg text-sm font-bold text-white"
              style={{ backgroundColor: color }}
            >
              {label.slice(0, 2).toUpperCase()}
            </div>
            <div>
              <p className="font-medium">{label}</p>
              <p className="text-xs text-gray-500">{description}</p>
            </div>
          </div>
          <Badge variant={statusVariants[status ?? 'inactive']}>
            {statusLabels[status ?? 'inactive']}
          </Badge>
        </div>

        {lastSyncAt && (
          <p className="text-xs text-gray-400">
            Синхронизировано: {new Date(lastSyncAt).toLocaleString('ru')}
          </p>
        )}

        <div className="flex gap-2">
          {connected ? (
            <Button
              variant="outline"
              size="sm"
              onClick={onDisconnect}
              className="border-red-200 text-red-600"
            >
              Отключить
            </Button>
          ) : (
            <Button size="sm" onClick={onConnect}>
              Подключить
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
