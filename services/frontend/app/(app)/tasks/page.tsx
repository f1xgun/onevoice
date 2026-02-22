'use client';

import { Fragment, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import { ListChecks, ChevronDown, ChevronRight } from 'lucide-react';
import { api } from '@/lib/api';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import type { AgentTask } from '@/types/task';

const statusLabels: Record<string, string> = {
  pending: 'В очереди',
  running: 'Выполняется',
  done: 'Завершено',
  error: 'Ошибка',
};

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive'> = {
  pending: 'secondary',
  running: 'secondary',
  done: 'default',
  error: 'destructive',
};

const platformLabels: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Яндекс',
};

function TaskSkeleton() {
  return (
    <TableRow>
      <TableCell>
        <Skeleton className="h-4 w-32" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-5 w-16 rounded-full" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-5 w-16 rounded-full" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-4 w-20" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-4 w-20" />
      </TableCell>
    </TableRow>
  );
}

function ExpandedRow({ task }: { task: AgentTask }) {
  return (
    <TableRow>
      <TableCell colSpan={5} className="bg-muted/30 p-4">
        <div className="space-y-3">
          {task.input != null && (
            <div>
              <p className="mb-1 text-xs font-medium text-muted-foreground">Входные данные</p>
              <pre className="max-h-40 overflow-auto rounded-md bg-muted p-3 text-xs">
                {JSON.stringify(task.input as object, null, 2)}
              </pre>
            </div>
          )}

          {task.output != null && (
            <div>
              <p className="mb-1 text-xs font-medium text-muted-foreground">Результат</p>
              <pre className="max-h-40 overflow-auto rounded-md bg-muted p-3 text-xs">
                {JSON.stringify(task.output as object, null, 2)}
              </pre>
            </div>
          )}

          {task.error && (
            <div>
              <p className="mb-1 text-xs font-medium text-muted-foreground">Ошибка</p>
              <p className="text-sm text-destructive">{task.error}</p>
            </div>
          )}

          {task.startedAt && (
            <p className="text-xs text-muted-foreground">
              Начало: {format(new Date(task.startedAt), 'd MMM yyyy HH:mm', { locale: ru })}
            </p>
          )}
          {task.completedAt && (
            <p className="text-xs text-muted-foreground">
              Завершено: {format(new Date(task.completedAt), 'd MMM yyyy HH:mm', { locale: ru })}
            </p>
          )}
        </div>
      </TableCell>
    </TableRow>
  );
}

export default function TasksPage() {
  const [status, setStatus] = useState<string>('all');
  const [platform, setPlatform] = useState<string>('all');
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const { data: tasks = [], isLoading } = useQuery<AgentTask[]>({
    queryKey: ['tasks', status, platform],
    queryFn: () => {
      const params = new URLSearchParams();
      if (status !== 'all') params.set('status', status);
      if (platform !== 'all') params.set('platform', platform);
      return api.get(`/tasks?${params}`).then((r) => r.data as AgentTask[]);
    },
  });

  return (
    <div className="max-w-4xl space-y-6 p-8">
      <div>
        <h1 className="mb-1 text-2xl font-bold">Задачи</h1>
        <p className="text-sm text-muted-foreground">Задачи, выполняемые агентами на платформах</p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Select value={platform} onValueChange={setPlatform}>
          <SelectTrigger className="w-[160px]">
            <SelectValue placeholder="Платформа" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все платформы</SelectItem>
            <SelectItem value="telegram">Telegram</SelectItem>
            <SelectItem value="vk">VK</SelectItem>
            <SelectItem value="yandex_business">Яндекс</SelectItem>
          </SelectContent>
        </Select>

        <Tabs value={status} onValueChange={setStatus}>
          <TabsList>
            <TabsTrigger value="all">Все</TabsTrigger>
            <TabsTrigger value="pending">В очереди</TabsTrigger>
            <TabsTrigger value="running">Активные</TabsTrigger>
            <TabsTrigger value="done">Завершены</TabsTrigger>
            <TabsTrigger value="error">Ошибки</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {isLoading && (
        <div className="duration-200 animate-in fade-in">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Тип</TableHead>
                <TableHead>Платформа</TableHead>
                <TableHead>Статус</TableHead>
                <TableHead>Создано</TableHead>
                <TableHead>Длительность</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {Array.from({ length: 5 }, (_, i) => (
                <TaskSkeleton key={i} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {!isLoading && tasks.length === 0 && (
        <div className="py-16 text-center duration-300 animate-in fade-in">
          <ListChecks className="mx-auto mb-3 h-10 w-10 text-muted-foreground/40" />
          <p className="text-muted-foreground">Задач пока нет</p>
        </div>
      )}

      {!isLoading && tasks.length > 0 && (
        <div className="duration-300 animate-in fade-in slide-in-from-bottom-2">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Тип</TableHead>
                <TableHead>Платформа</TableHead>
                <TableHead>Статус</TableHead>
                <TableHead>Создано</TableHead>
                <TableHead>Длительность</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tasks.map((task) => {
                const isExpanded = expandedId === task.id;
                let duration = '';
                if (task.startedAt && task.completedAt) {
                  const ms =
                    new Date(task.completedAt).getTime() - new Date(task.startedAt).getTime();
                  duration = ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`;
                }

                return (
                  <Fragment key={task.id}>
                    <TableRow
                      className="cursor-pointer"
                      onClick={() => setExpandedId(isExpanded ? null : task.id)}
                    >
                      <TableCell>
                        <div className="flex items-center gap-2">
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                          ) : (
                            <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                          )}
                          <span className="text-sm font-medium">{task.type}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="text-xs">
                          {platformLabels[task.platform] ?? task.platform}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <Badge variant={statusVariants[task.status] ?? 'secondary'}>
                          {statusLabels[task.status] ?? task.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {format(new Date(task.createdAt), 'd MMM yyyy HH:mm', {
                          locale: ru,
                        })}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {duration || '—'}
                      </TableCell>
                    </TableRow>
                    {isExpanded && <ExpandedRow task={task} />}
                  </Fragment>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
