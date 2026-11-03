'use client';

import { useState } from 'react';
import Image from 'next/image';
import { ChevronRight } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { ProfileForm } from './ProfileForm';
import { HoursForm, SpecialDatesForm } from './ScheduleForm';
import { BusinessPresence } from './BusinessPresence';
import { groupOpeningHours, openingHours } from '@/lib/business-schedule';
import { businessMark } from '@/lib/business-display';
import { BUSINESS_DESCRIPTION_MAX_LEN } from '@/lib/schemas';
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
    openingHours(day, t('dayOff'));
  const groups = groupOpeningHours(schedule ?? []);
  function groupLabel(keys: ScheduleDay['day'][]) {
    if (keys.length === 1) return days(keys[0]);
    const last =
      locale === 'ru'
        ? days(keys[keys.length - 1]).toLocaleLowerCase(locale)
        : days(keys[keys.length - 1]);
    return keys.length === 2 ? `${days(keys[0])}, ${last}` : `${days(keys[0])} – ${last}`;
  }

  function row(label: string, value: string | null | undefined, target: Editor, stacked = false) {
    return (
      <button
        type="button"
        key={label}
        onClick={() => setEditor(target)}
        className="flex min-h-[52px] w-full items-center gap-3 px-block py-3 text-left md:px-block-desktop"
      >
        {stacked ? (
          <span className="min-w-0 flex-1">
            <span className="block">{label}</span>
            <span className="mt-1 block break-words text-[15px] leading-[22px] text-ink-soft">
              {value || t('empty')}
            </span>
          </span>
        ) : (
          <>
            <span className="shrink-0">{label}</span>
            <span className="ml-auto min-w-0 break-words text-right text-ink-soft">
              {value || t('empty')}
            </span>
          </>
        )}
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
            <div className="overflow-hidden rounded-block bg-card py-2">
              {(['name', 'category', 'address', 'phone', 'website'] as const).map((field) =>
                row(
                  field === 'address' ? t('address') : fields(field),
                  field === 'website'
                    ? business.website?.replace(/^https?:\/\//, '').replace(/\/$/, '')
                    : business[field],
                  'profile',
                  field === 'address'
                )
              )}
            </div>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('hours')}</h2>
            <div className="rounded-block bg-card py-2">
              {schedule?.length
                ? groups.map((group) => row(groupLabel(group.days), hours(group.hours), 'hours'))
                : row(t('hours'), undefined, 'hours')}
            </div>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('specialDates')}</h2>
            <div className="rounded-block bg-card py-2">
              {specialDates.map((day) =>
                row(
                  new Intl.DateTimeFormat(locale, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'long',
                    timeZone: 'UTC',
                  })
                    .format(new Date(day.date))
                    .replace(/^./u, (letter) => letter.toLocaleUpperCase(locale)),
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
              <span className="block">{business.description || t('empty')}</span>
              <span className="mt-3 block text-meta text-ink-soft">
                {t('descriptionCount', {
                  count: business.description?.length ?? 0,
                  max: BUSINESS_DESCRIPTION_MAX_LEN,
                })}
              </span>
            </button>
          </section>
          <section>
            <h2 className="mb-2.5 text-group-title">{t('logo')}</h2>
            <button
              type="button"
              onClick={() => setEditor('profile')}
              className="flex min-h-20 w-full items-center gap-3 rounded-block bg-card p-block text-left text-brand md:p-block-desktop"
            >
              {business.logoUrl ? (
                <Image
                  src={business.logoUrl}
                  alt={business.name}
                  width={44}
                  height={44}
                  unoptimized
                  className="h-11 w-11 shrink-0 rounded-inner object-cover"
                />
              ) : (
                <span
                  aria-hidden
                  className="flex h-11 w-11 shrink-0 items-center justify-center rounded-inner bg-brand text-lg font-semibold text-brand-foreground"
                >
                  {businessMark(business.name)}
                </span>
              )}
              {t('editPhoto')}
            </button>
          </section>
        </div>
        <BusinessPresence
          business={business}
          businessId={businessId}
          schedule={schedule ?? []}
          specialDates={specialDates}
        />
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
