'use client';

// Linen rebuild — Phase 4.8.
// Business profile page. Layout: PageHeader + two columns on lg+ (forms
// left, sticky AI-understanding rail right). Each form section is a
// paper-raised card with a MonoLabel caption + section title. Mutations
// are owned per section (ProfileForm, HoursForm, SpecialDatesForm,
// VoiceToneSection) so a save in one section doesn't block another.

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { isAxiosError } from 'axios';
import { api } from '@/lib/api';
import { ProfileForm } from '@/components/business/ProfileForm';
import { HoursForm, SpecialDatesForm } from '@/components/business/ScheduleForm';
import { VoiceToneSection } from '@/components/business/VoiceToneSection';
import { AISummaryRail } from '@/components/business/AISummaryRail';
import { PageHeader } from '@/components/ui/page-header';
import { MonoLabel } from '@/components/ui/mono-label';
import { Skeleton } from '@/components/ui/skeleton';
import { normalizeStoredTones, type ToneId } from '@/lib/tones';
import type { Business, ScheduleDay, SpecialDate } from '@/types/business';

function BusinessSkeleton() {
  return (
    <>
      <PageHeader
        title="Профиль бизнеса"
        sub="Чем подробнее вы расскажете о себе, тем точнее AI будет говорить вашим голосом."
      />
      <div className="grid grid-cols-1 gap-8 px-4 pb-10 sm:px-12 sm:pb-16 lg:grid-cols-[1fr_320px]">
        <div className="flex flex-col gap-6">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-line bg-paper-raised p-6">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="mt-2 h-6 w-48" />
              <div className="mt-5 flex flex-col gap-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            </div>
          ))}
        </div>
        <div className="flex flex-col gap-3">
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    </>
  );
}

export default function BusinessPage() {
  const { data, isLoading, isError, error } = useQuery<Business>({
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data as Business),
    retry: false,
  });

  // Persisted in business.settings.voiceTone as stable enum ids
  // ("warm", "businesslike", …). normalizeStoredTones() also accepts the
  // legacy Russian-label form so pre-migration records ("Деловой") still
  // light up the right chips — the next save flushes the canonical form.
  const persistedTones = useMemo<ToneId[]>(
    () => normalizeStoredTones(data?.settings?.voiceTone),
    [data?.settings?.voiceTone]
  );
  const [tones, setTones] = useState<ToneId[]>(persistedTones);
  // Sync local state when the underlying business record changes.
  useEffect(() => setTones(persistedTones), [persistedTones]);

  const is404 = isError && isAxiosError(error) && error.response?.status === 404;

  if (isLoading) return <BusinessSkeleton />;
  if (isError && !is404) {
    return (
      <>
        <PageHeader title="Профиль бизнеса" />
        <div className="px-4 pb-10 sm:px-12 sm:pb-16">
          <div className="rounded-lg border border-[var(--ov-danger)]/40 bg-[var(--ov-danger-soft)] p-6 text-sm text-[var(--ov-danger)]">
            Не получилось загрузить данные. Попробуйте обновить страницу.
          </div>
        </div>
      </>
    );
  }

  const isCreateMode = is404;
  const title = isCreateMode ? 'Создайте профиль бизнеса' : 'Профиль бизнеса';
  const sub = isCreateMode
    ? 'Заполните «Основное», и появятся остальные разделы. OneVoice использует это, когда пишет от вашего имени.'
    : 'Чем подробнее вы расскажете о себе, тем точнее AI будет говорить вашим голосом.';

  const schedule = data?.settings?.schedule as
    | { schedule?: ScheduleDay[]; specialDates?: SpecialDate[] }
    | ScheduleDay[]
    | undefined;
  // The /business/schedule handler stores the whole {schedule, specialDates}
  // body under settings.schedule. Older records may store just the array —
  // handle both shapes defensively.
  const initialSchedule = Array.isArray(schedule) ? schedule : schedule?.schedule;
  const initialSpecialDates = Array.isArray(schedule) ? undefined : schedule?.specialDates;

  return (
    <>
      <PageHeader title={title} sub={sub} />

      <div className="grid grid-cols-1 gap-8 px-4 pb-10 sm:px-12 sm:pb-16 lg:grid-cols-[1fr_320px]">
        {/* Main column */}
        <div className="flex flex-col gap-6">
          <Section caption="Основное" title="О бизнесе" sub="Имя, контакты и описание для ИИ-ассистента.">
            <ProfileForm defaultValues={isCreateMode ? undefined : data} />
          </Section>

          {!isCreateMode && (
            <>
              <Section
                caption="Голос"
                title="Голос и тон"
                sub="Как ИИ должен звучать от вашего имени."
              >
                <VoiceToneSection initial={tones} onChange={setTones} />
              </Section>

              <Section
                caption="Часы"
                title="Часы работы"
                sub="Используется, когда клиенты спрашивают «вы открыты?»."
              >
                <HoursForm
                  initialSchedule={initialSchedule}
                  initialSpecialDates={initialSpecialDates}
                />
              </Section>

              <Section
                caption="Особые даты"
                title="Праздники и особый режим"
                sub="Что ИИ должен учесть — праздники, ремонты, корпоративы."
              >
                <SpecialDatesForm
                  initialSchedule={initialSchedule}
                  initialSpecialDates={initialSpecialDates}
                />
              </Section>
            </>
          )}
        </div>

        {/* Right rail */}
        <AISummaryRail business={isCreateMode ? undefined : data} tones={tones} />
      </div>
    </>
  );
}

function Section({
  caption,
  title,
  sub,
  children,
}: {
  caption: string;
  title: string;
  sub?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-lg border border-line bg-paper-raised">
      <header className="border-b border-line-soft px-6 py-4">
        <MonoLabel>{caption}</MonoLabel>
        <h2 className="mt-1 text-lg font-medium tracking-tight text-ink">{title}</h2>
        {sub && <p className="mt-1 text-[13px] text-ink-mid">{sub}</p>}
      </header>
      <div className="px-6 py-5">{children}</div>
    </section>
  );
}
