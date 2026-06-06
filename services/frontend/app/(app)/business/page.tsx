'use client';

// Linen rebuild.
// Business profile page. Layout: PageHeader + two columns on lg+ (forms
// left, sticky AI-understanding rail right). Each form section is a
// paper-raised card with a MonoLabel caption + section title. Mutations
// are owned per section (ProfileForm, HoursForm, SpecialDatesForm,
// VoiceToneSection) so a save in one section doesn't block another.

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { isAxiosError } from 'axios';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
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
  const tBusiness = useTranslations('business');
  return (
    <>
      <PageHeader title={tBusiness('title')} sub={tBusiness('subtitle')} />
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
  const tBusiness = useTranslations('business');
  const tSections = useTranslations('business.sections');
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const { data, isLoading, isError, error } = useQuery<Business>({
    queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId),
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get<Business>(BIZ_API_PATHS.BUSINESS.ROOT)
        .then((r) => r.data),
    enabled: !!activeBusinessId,
    retry: false,
  });

  const persistedTones = useMemo<ToneId[]>(
    () => normalizeStoredTones(data?.settings?.voiceTone),
    [data?.settings?.voiceTone]
  );
  const [tones, setTones] = useState<ToneId[]>(persistedTones);
  useEffect(() => setTones(persistedTones), [persistedTones]);

  const is404 = isError && isAxiosError(error) && error.response?.status === HTTP_STATUS.NOT_FOUND;

  if (isLoading) return <BusinessSkeleton />;
  if (isError && !is404) {
    return (
      <>
        <PageHeader title={tBusiness('title')} />
        <div className="px-4 pb-10 sm:px-12 sm:pb-16">
          <div className="border-[var(--ov-danger)]/40 rounded-lg border bg-[var(--ov-danger-soft)] p-6 text-sm text-[var(--ov-danger)]">
            {tBusiness('errorLoad')}
          </div>
        </div>
      </>
    );
  }

  const isCreateMode = is404;
  const title = isCreateMode ? tBusiness('createTitle') : tBusiness('title');
  const sub = isCreateMode ? tBusiness('createSubtitle') : tBusiness('subtitle');

  const schedule = data?.settings?.schedule as
    | { schedule?: ScheduleDay[]; specialDates?: SpecialDate[] }
    | ScheduleDay[]
    | undefined;
  const initialSchedule = Array.isArray(schedule) ? schedule : schedule?.schedule;
  const initialSpecialDates = Array.isArray(schedule) ? undefined : schedule?.specialDates;

  return (
    <>
      <PageHeader title={title} sub={sub} />

      <div className="grid grid-cols-1 gap-8 px-4 pb-10 sm:px-12 sm:pb-16 lg:grid-cols-[1fr_320px]">
        {/* Main column */}
        <div className="flex flex-col gap-6">
          <Section
            caption={tSections('profile.caption')}
            title={tSections('profile.title')}
            sub={tSections('profile.sub')}
          >
            <ProfileForm defaultValues={isCreateMode ? undefined : data} />
          </Section>

          {!isCreateMode && (
            <>
              <Section
                caption={tSections('voice.caption')}
                title={tSections('voice.title')}
                sub={tSections('voice.sub')}
              >
                <VoiceToneSection initial={tones} onChange={setTones} />
              </Section>

              <Section
                caption={tSections('hours.caption')}
                title={tSections('hours.title')}
                sub={tSections('hours.sub')}
              >
                <HoursForm
                  initialSchedule={initialSchedule}
                  initialSpecialDates={initialSpecialDates}
                />
              </Section>

              <Section
                caption={tSections('specialDates.caption')}
                title={tSections('specialDates.title')}
                sub={tSections('specialDates.sub')}
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
