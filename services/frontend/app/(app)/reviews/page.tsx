'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Star, MessageSquare, Send } from 'lucide-react';
import { api } from '@/lib/api';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { Review } from '@/types/review';

const platformLabels: Record<string, string> = {
  yandex_business: 'Яндекс',
  google: 'Google',
  '2gis': '2ГИС',
};

const platformColors: Record<string, string> = {
  yandex_business: 'bg-yellow-100 text-yellow-800',
  google: 'bg-blue-100 text-blue-800',
  '2gis': 'bg-green-100 text-green-800',
};

const replyStatusLabels: Record<string, string> = {
  pending: 'Без ответа',
  replied: 'Ответ отправлен',
  error: 'Ошибка',
};

function StarRating({ rating }: { rating: number }) {
  return (
    <div className="flex items-center gap-0.5">
      {Array.from({ length: 5 }, (_, i) => (
        <Star
          key={i}
          className={`h-4 w-4 ${i < rating ? 'fill-yellow-400 text-yellow-400' : 'text-gray-200'}`}
        />
      ))}
    </div>
  );
}

function ReviewSkeleton() {
  return (
    <Card>
      <CardContent className="space-y-3 p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Skeleton className="h-5 w-16 rounded-full" />
            <Skeleton className="h-4 w-24" />
          </div>
          <Skeleton className="h-4 w-20" />
        </div>
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-3/4" />
      </CardContent>
    </Card>
  );
}

export default function ReviewsPage() {
  const qc = useQueryClient();
  const [platform, setPlatform] = useState<string>('all');
  const [replyStatus, setReplyStatus] = useState<string>('all');
  const [replyDialog, setReplyDialog] = useState<Review | null>(null);
  const [replyText, setReplyText] = useState('');

  const { data: reviews = [], isLoading } = useQuery<Review[]>({
    queryKey: ['reviews', platform, replyStatus],
    queryFn: () => {
      const params = new URLSearchParams();
      if (platform !== 'all') params.set('platform', platform);
      if (replyStatus !== 'all') params.set('reply_status', replyStatus);
      return api.get(`/reviews?${params}`).then((r) => r.data as Review[]);
    },
  });

  const replyMutation = useMutation({
    mutationFn: ({ id, text }: { id: string; text: string }) =>
      api.put(`/reviews/${id}/reply`, { replyText: text }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['reviews'] });
      toast.success('Ответ отправлен');
      setReplyDialog(null);
      setReplyText('');
    },
    onError: () => toast.error('Ошибка отправки ответа'),
  });

  function openReply(review: Review) {
    setReplyDialog(review);
    setReplyText(review.replyText ?? '');
  }

  return (
    <div className="max-w-3xl space-y-6 p-8">
      <div>
        <h1 className="mb-1 text-2xl font-bold">Отзывы</h1>
        <p className="text-sm text-muted-foreground">Управляйте отзывами с подключённых платформ</p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Select value={platform} onValueChange={setPlatform}>
          <SelectTrigger className="w-[160px]">
            <SelectValue placeholder="Платформа" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все платформы</SelectItem>
            <SelectItem value="yandex_business">Яндекс</SelectItem>
            <SelectItem value="google">Google</SelectItem>
            <SelectItem value="2gis">2ГИС</SelectItem>
          </SelectContent>
        </Select>

        <Tabs value={replyStatus} onValueChange={setReplyStatus}>
          <TabsList>
            <TabsTrigger value="all">Все</TabsTrigger>
            <TabsTrigger value="pending">Без ответа</TabsTrigger>
            <TabsTrigger value="replied">С ответом</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {isLoading && (
        <div className="space-y-4">
          {Array.from({ length: 3 }, (_, i) => (
            <ReviewSkeleton key={i} />
          ))}
        </div>
      )}

      {!isLoading && reviews.length === 0 && (
        <div className="py-16 text-center">
          <MessageSquare className="mx-auto mb-3 h-10 w-10 text-muted-foreground/40" />
          <p className="text-muted-foreground">Отзывов пока нет</p>
        </div>
      )}

      {!isLoading && reviews.length > 0 && (
        <div className="space-y-4">
          {reviews.map((review) => (
            <Card key={review.id}>
              <CardContent className="space-y-3 p-5">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="secondary" className={platformColors[review.platform] ?? ''}>
                      {platformLabels[review.platform] ?? review.platform}
                    </Badge>
                    <span className="text-sm font-medium">{review.authorName}</span>
                    <StarRating rating={review.rating} />
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {new Date(review.createdAt).toLocaleDateString('ru-RU')}
                  </span>
                </div>

                <p className="text-sm leading-relaxed">{review.text}</p>

                {review.replyText && (
                  <div className="rounded-md bg-muted/50 p-3">
                    <p className="mb-1 text-xs font-medium text-muted-foreground">Ваш ответ</p>
                    <p className="text-sm">{review.replyText}</p>
                  </div>
                )}

                <div className="flex items-center justify-between pt-1">
                  <Badge variant={review.replyStatus === 'replied' ? 'default' : 'secondary'}>
                    {replyStatusLabels[review.replyStatus] ?? review.replyStatus}
                  </Badge>
                  <Button variant="outline" size="sm" onClick={() => openReply(review)}>
                    <Send className="mr-1.5 h-3.5 w-3.5" />
                    {review.replyText ? 'Изменить ответ' : 'Ответить'}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={!!replyDialog} onOpenChange={(open) => !open && setReplyDialog(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Ответ на отзыв</DialogTitle>
          </DialogHeader>
          {replyDialog && (
            <div className="space-y-4">
              <div className="rounded-md bg-muted/50 p-3">
                <div className="mb-1 flex items-center gap-2">
                  <span className="text-sm font-medium">{replyDialog.authorName}</span>
                  <StarRating rating={replyDialog.rating} />
                </div>
                <p className="text-sm">{replyDialog.text}</p>
              </div>
              <Textarea
                value={replyText}
                onChange={(e) => setReplyText(e.target.value)}
                placeholder="Введите ответ..."
                rows={4}
              />
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setReplyDialog(null)}>
              Отмена
            </Button>
            <Button
              onClick={() =>
                replyDialog && replyMutation.mutate({ id: replyDialog.id, text: replyText })
              }
              disabled={!replyText.trim() || replyMutation.isPending}
            >
              {replyMutation.isPending ? 'Отправка...' : 'Отправить'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
