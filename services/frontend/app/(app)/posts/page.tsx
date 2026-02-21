'use client';

import { Fragment, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { format } from 'date-fns';
import { ru } from 'date-fns/locale';
import { FileText, ChevronDown, ChevronRight } from 'lucide-react';
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
import type { Post } from '@/types/post';

const statusLabels: Record<string, string> = {
  draft: 'Черновик',
  scheduled: 'Запланирован',
  published: 'Опубликован',
  error: 'Ошибка',
};

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive'> = {
  draft: 'secondary',
  scheduled: 'secondary',
  published: 'default',
  error: 'destructive',
};

const platformLabels: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'VK',
  yandex_business: 'Яндекс',
};

function PostSkeleton() {
  return (
    <TableRow>
      <TableCell><Skeleton className="h-4 w-48" /></TableCell>
      <TableCell><Skeleton className="h-5 w-16 rounded-full" /></TableCell>
      <TableCell><Skeleton className="h-4 w-24" /></TableCell>
      <TableCell><Skeleton className="h-4 w-20" /></TableCell>
    </TableRow>
  );
}

function ExpandedRow({ post }: { post: Post }) {
  const results = post.platformResults ? Object.entries(post.platformResults) : [];

  return (
    <TableRow>
      <TableCell colSpan={4} className="bg-muted/30 p-4">
        <div className="space-y-3">
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">Полный текст</p>
            <p className="whitespace-pre-wrap text-sm">{post.content}</p>
          </div>

          {post.mediaUrls && post.mediaUrls.length > 0 && (
            <div>
              <p className="mb-1 text-xs font-medium text-muted-foreground">Медиа</p>
              <p className="text-sm text-muted-foreground">
                {post.mediaUrls.length} файл(ов)
              </p>
            </div>
          )}

          {results.length > 0 && (
            <div>
              <p className="mb-1 text-xs font-medium text-muted-foreground">
                Результаты по платформам
              </p>
              <div className="space-y-1">
                {results.map(([platform, result]) => (
                  <div
                    key={platform}
                    className="flex items-center gap-2 text-sm"
                  >
                    <span className="font-medium">
                      {platformLabels[platform] ?? platform}:
                    </span>
                    <Badge
                      variant={result.status === 'published' ? 'default' : 'destructive'}
                      className="text-xs"
                    >
                      {result.status === 'published' ? 'OK' : result.status}
                    </Badge>
                    {result.error && (
                      <span className="text-xs text-destructive">{result.error}</span>
                    )}
                    {result.url && (
                      <a
                        href={result.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-xs text-primary underline"
                      >
                        Ссылка
                      </a>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </TableCell>
    </TableRow>
  );
}

export default function PostsPage() {
  const [status, setStatus] = useState<string>('all');
  const [platform, setPlatform] = useState<string>('all');
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const { data: posts = [], isLoading } = useQuery<Post[]>({
    queryKey: ['posts', status, platform],
    queryFn: () => {
      const params = new URLSearchParams();
      if (status !== 'all') params.set('status', status);
      if (platform !== 'all') params.set('platform', platform);
      return api.get(`/posts?${params}`).then((r) => r.data as Post[]);
    },
  });

  return (
    <div className="max-w-4xl space-y-6 p-8">
      <div>
        <h1 className="mb-1 text-2xl font-bold">Посты</h1>
        <p className="text-sm text-muted-foreground">
          Все публикации на подключённых платформах
        </p>
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
            <TabsTrigger value="published">Опубликованы</TabsTrigger>
            <TabsTrigger value="scheduled">Запланированы</TabsTrigger>
            <TabsTrigger value="error">Ошибки</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {isLoading && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Контент</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Платформы</TableHead>
              <TableHead>Дата</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 5 }, (_, i) => (
              <PostSkeleton key={i} />
            ))}
          </TableBody>
        </Table>
      )}

      {!isLoading && posts.length === 0 && (
        <div className="py-16 text-center">
          <FileText className="mx-auto mb-3 h-10 w-10 text-muted-foreground/40" />
          <p className="text-muted-foreground">Постов пока нет</p>
        </div>
      )}

      {!isLoading && posts.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Контент</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Платформы</TableHead>
              <TableHead>Дата</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {posts.map((post) => {
              const isExpanded = expandedId === post.id;
              const platforms = post.platformResults
                ? Object.keys(post.platformResults)
                : [];

              return (
                <Fragment key={post.id}>
                  <TableRow
                    className="cursor-pointer"
                    onClick={() => setExpandedId(isExpanded ? null : post.id)}
                  >
                    <TableCell className="max-w-xs">
                      <div className="flex items-center gap-2">
                        {isExpanded ? (
                          <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                        ) : (
                          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                        )}
                        <span className="truncate text-sm">{post.content}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusVariants[post.status] ?? 'secondary'}>
                        {statusLabels[post.status] ?? post.status}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {platforms.map((p) => (
                          <Badge key={p} variant="outline" className="text-xs">
                            {platformLabels[p] ?? p}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {format(new Date(post.publishedAt ?? post.createdAt), 'd MMM yyyy', {
                        locale: ru,
                      })}
                    </TableCell>
                  </TableRow>
                  {isExpanded && <ExpandedRow post={post} />}
                </Fragment>
              );
            })}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
