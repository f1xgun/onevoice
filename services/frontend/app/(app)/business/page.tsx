'use client';

import { useQuery } from '@tanstack/react-query';
import { isAxiosError } from 'axios';
import { api } from '@/lib/api';
import { ProfileForm } from '@/components/business/ProfileForm';
import { ScheduleForm } from '@/components/business/ScheduleForm';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import type { Business, ScheduleDay } from '@/types/business';

function BusinessSkeleton() {
  return (
    <div className="max-w-2xl space-y-8 p-8 duration-200 animate-in fade-in">
      <div className="space-y-2">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
      </div>
      <div className="space-y-4">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-10 w-32" />
      </div>
    </div>
  );
}

export default function BusinessPage() {
  const { data, isLoading, isError, error } = useQuery<Business>({
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data as Business),
    retry: false,
  });

  const is404 = isError && isAxiosError(error) && error.response?.status === 404;

  if (isLoading) return <BusinessSkeleton />;
  if (isError && !is404) return <div className="p-8 text-red-500">Ошибка загрузки данных</div>;

  const isCreateMode = is404;
  const title = isCreateMode ? 'Создайте профиль бизнеса' : 'Профиль бизнеса';
  const subtitle = isCreateMode
    ? 'Заполните информацию о вашем бизнесе, чтобы начать работу'
    : 'Эта информация используется ИИ при общении с клиентами';

  return (
    <div className="max-w-2xl space-y-8 p-8 duration-300 animate-in fade-in slide-in-from-bottom-2">
      <div>
        <h1 className="mb-1 text-2xl font-bold">{title}</h1>
        <p className="text-sm text-gray-500">{subtitle}</p>
      </div>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Основная информация</h2>
        <ProfileForm defaultValues={isCreateMode ? undefined : data} />
      </section>

      {!isCreateMode && (
        <>
          <Separator />

          <section className="space-y-4">
            <h2 className="text-lg font-semibold">Расписание работы</h2>
            <ScheduleForm initialSchedule={data?.settings?.schedule as ScheduleDay[] | undefined} />
          </section>
        </>
      )}
    </div>
  );
}
