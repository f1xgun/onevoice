'use client';

import { useState } from 'react';
import Image from 'next/image';
import { ChevronRight } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { ProfileForm } from './ProfileForm';
import { HoursForm, SpecialDatesForm } from './ScheduleForm';
import { BusinessComparison } from './BusinessComparison';
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetDescription,
} from '@/components/design-system/AppSheet';
import type { Business, ScheduleDay, SpecialDate } from '@/types/business';

interface BusinessCardProps {
  business: Business;
  businessId: string;
}
type Editor = 'profile' | 'hours' | 'specialDates' | null;

export function BusinessCard({ business, businessId }: BusinessCardProps) {
  const t = useTranslations('business.card');
  const locale = useLocale();
  const fields = useTranslations('business.profileForm.fields');
  const days = useTranslations('business.daysOfWeek');
  const scheduleText = useTranslations('business.scheduleForm');
  const specialDatesEditor: Editor = 'specialDates';
  const [editor, setEditor] = useState<Editor>(null);
  const raw = business.settings?.schedule as
    | ScheduleDay[]
    | { schedule?: ScheduleDay[]; specialDates?: SpecialDate[] }
    | undefined;
  const schedule = Array.isArray(raw) ? raw : raw?.schedule;
  const specialDates = Array.isArray(raw)
    ? ((business.settings?.specialDates as SpecialDate[] | undefined) ?? [])
    : (raw?.specialDates ?? (business.settings?.specialDates as SpecialDate[] | undefined) ?? []);
  const hours = (day: { closed: boolean; open?: string; close?: string }) =>
    day.closed ? scheduleText('closed') : `${day.open ?? '—'}–${day.close ?? '—'}`;

  function row(label: string, value: string | null | undefined, target: Editor) {
    return (
      <button
        type="button"
        key={label}
        onClick={() => setEditor(target)}
        className="flex min-h-[52px] w-full items-center gap-3 px-block py-3 text-left md:px-block-desktop"
      >
        <span className="shrink-0">{label}</span>
        <span className="ml-auto min-w-0 break-words text-right text-ink-soft">
          {value || t('empty')}
        </span>
        <ChevronRight aria-hidden className="h-4 w-4 shrink-0 text-ink-soft" />
      </button>
    );
  }

  return (
    <>
      <div className="grid min-w-0 grid-cols-1 gap-8 xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
        <div className="min-w-0 space-y-group">
          <BusinessComparison businessId={businessId} />
          <section>
            <h2 className="mb-2.5 text-group-title">{t('basics')}</h2>
            <div className="overflow-hidden rounded-block bg-card">
              {(['name', 'category', 'address', 'phone', 'website'] as const).map((field) =>
                row(fields(field), business[field], 'profile')
              )}
            </div>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('hours')}</h2>
            <div className="rounded-block bg-card">
              {schedule?.length
                ? schedule.map((day) => row(days(day.day), hours(day), 'hours'))
                : row(t('hours'), undefined, 'hours')}
            </div>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('specialDates')}</h2>
            <div className="rounded-block bg-card">
              {specialDates.map((day) =>
                row(
                  new Intl.DateTimeFormat(locale, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'long',
                    timeZone: 'UTC',
                  }).format(new Date(day.date)),
                  hours(day),
                  specialDatesEditor
                )
              )}
              <button
                type="button"
                onClick={() => setEditor('specialDates')}
                className="min-h-control px-block py-3 text-brand md:px-block-desktop"
              >
                {t('addDate')}
              </button>
            </div>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{fields('description')}</h2>
            <button
              type="button"
              onClick={() => setEditor('profile')}
              className="w-full whitespace-pre-wrap break-words rounded-block bg-card p-block text-left leading-6 md:p-block-desktop"
            >
              {business.description || t('empty')}
            </button>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('photo')}</h2>
            <button
              type="button"
              onClick={() => setEditor('profile')}
              className="flex min-h-[120px] w-full items-center gap-4 rounded-block bg-card p-block text-left text-brand md:p-block-desktop"
            >
              {business.logoUrl && (
                <Image
                  src={business.logoUrl}
                  alt={business.name}
                  width={120}
                  height={120}
                  unoptimized
                  className="h-[120px] w-[120px] rounded-inner object-cover"
                />
              )}
              {t('editPhoto')}
            </button>
          </section>
        </div>
        <aside className="hidden min-w-0 xl:block">
          <h2 className="mb-2.5 text-group-title">{t('preview')}</h2>
          <div className="space-y-3 break-words rounded-block bg-card p-block-desktop">
            {business.logoUrl && (
              <Image
                src={business.logoUrl}
                alt=""
                width={280}
                height={160}
                unoptimized
                className="h-40 w-full rounded-inner object-cover"
              />
            )}
            <h3 className="text-lg font-semibold">{business.name}</h3>
            <p className="text-ink-soft">
              {business.category} · {business.address}
            </p>
            <p>{business.phone}</p>
            <p className="text-ink-soft">{business.website}</p>
          </div>
          <p className="mt-3 text-meta text-ink-soft">{t('previewHint')}</p>
        </aside>
      </div>
      <Sheet
        open={editor !== null}
        onOpenChange={(open) => {
          if (!open) setEditor(null);
        }}
      >
        <SheetContent side="bottom">
          <SheetTitle>{t(editor ?? 'profile')}</SheetTitle>
          <SheetDescription className="mb-5 mt-2">
            {t(editor === 'specialDates' ? 'specialDestination' : 'destination')}
          </SheetDescription>
          {editor === 'profile' && <ProfileForm key={businessId} defaultValues={business} />}
          {editor === 'hours' && (
            <HoursForm
              key={businessId}
              initialSchedule={schedule}
              initialSpecialDates={specialDates}
            />
          )}
          {editor === 'specialDates' && (
            <SpecialDatesForm
              key={businessId}
              initialSchedule={schedule}
              initialSpecialDates={specialDates}
            />
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}
