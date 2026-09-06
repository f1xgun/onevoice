'use client';

// Linen rebuild.
// Splits the previous single block into two API-aware sections:
//   <HoursForm />          — weekly hours (Часы работы)
//   <SpecialDatesForm />   — date overrides (Особые даты)
// Both share the same PUT /business/schedule mutation: backend stores the
// `{schedule, specialDates}` payload under business.settings.schedule. Each
// section owns ONLY its own slice; the other slice is re-read from the freshest
// BUSINESS_PROFILE query cache at save time so a Hours save never clobbers a
// just-saved Special Dates value (and vice versa) across the two independent
// form instances.

import { useState, useEffect, useMemo, useRef } from 'react';
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { format, parseISO } from 'date-fns';
import { CalendarIcon, X } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';
import { useBusinessStore } from '@/lib/stores/business';
import { usePermission } from '@/lib/hooks/usePermission';
import { PermissionLoadError } from '@/components/permission/PermissionLoadError';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { Switch } from '@/components/ui/switch';
import { AppCalendar as Calendar } from '@/components/design-system/AppCalendar';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { MonoLabel } from '@/components/ui/mono-label';
import { Badge } from '@/components/ui/badge';
import type { Business, ScheduleDay, SpecialDate } from '@/types/business';

const DEFAULT_SCHEDULE: ScheduleDay[] = [
  { day: 'mon', open: '09:00', close: '21:00', closed: false },
  { day: 'tue', open: '09:00', close: '21:00', closed: false },
  { day: 'wed', open: '09:00', close: '21:00', closed: false },
  { day: 'thu', open: '09:00', close: '21:00', closed: false },
  { day: 'fri', open: '09:00', close: '21:00', closed: false },
  { day: 'sat', open: '10:00', close: '21:00', closed: false },
  { day: 'sun', open: '10:00', close: '20:00', closed: true },
];

// Day-of-week labels for the weekly hours grid. Built inside each
// consumer via useDayLabels() so a locale switch swaps the row labels.
function useDayLabels(): Record<ScheduleDay['day'], string> {
  const tDays = useTranslations('business.daysOfWeek');
  return useMemo(
    () => ({
      mon: tDays('mon'),
      tue: tDays('tue'),
      wed: tDays('wed'),
      thu: tDays('thu'),
      fri: tDays('fri'),
      sat: tDays('sat'),
      sun: tDays('sun'),
    }),
    [tDays]
  );
}

interface SchedulePayload {
  schedule: ScheduleDay[];
  specialDates: SpecialDate[];
}

// Which schedule slice a form instance owns. The non-owned slice is merged
// from the freshest BUSINESS_PROFILE cache at save time, never from the form's
// frozen local copy — that is what prevents the cross-form lost-update.
type ScheduleSlice = 'schedule' | 'specialDates';

// Reads the live schedule slices out of the cached Business profile, handling
// both the legacy bare-array shape and the `{schedule, specialDates}` object.
// Returns undefined for a slice when the cache has nothing for it so callers
// fall back to the form's own value.
function readCachedSchedule(
  queryClient: QueryClient,
  businessId: string | null
): { schedule?: ScheduleDay[]; specialDates?: SpecialDate[] } {
  const cached = queryClient.getQueryData<Business>(QUERY_KEYS.BUSINESS_PROFILE(businessId));
  const raw = cached?.settings?.schedule as
    | { schedule?: ScheduleDay[]; specialDates?: SpecialDate[] }
    | ScheduleDay[]
    | undefined;
  if (Array.isArray(raw)) return { schedule: raw, specialDates: undefined };
  return { schedule: raw?.schedule, specialDates: raw?.specialDates };
}

function useSchedule(initialSchedule?: ScheduleDay[], initialSpecialDates?: SpecialDate[]) {
  const [schedule, setSchedule] = useState<ScheduleDay[]>(initialSchedule ?? DEFAULT_SCHEDULE);
  const [specialDates, setSpecialDates] = useState<SpecialDate[]>(initialSpecialDates ?? []);
  const initialized = useRef(false);
  useEffect(() => {
    if (initialized.current) return;
    if (initialSchedule && initialSchedule.length > 0) setSchedule(initialSchedule);
    if (initialSpecialDates) setSpecialDates(initialSpecialDates);
    if (initialSchedule || initialSpecialDates) initialized.current = true;
  }, [initialSchedule, initialSpecialDates]);
  return { schedule, setSchedule, specialDates, setSpecialDates };
}

// owns names the slice this form edits. The PUT keeps the owned slice from the
// caller's `data` and overrides the other slice with the freshest cached value,
// so two independent form instances never overwrite each other's last save.
function useScheduleMutation(owns: ScheduleSlice, successMsg: string, errorMsg: string) {
  const queryClient = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  return useMutation({
    mutationFn: (data: SchedulePayload) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      const cached = readCachedSchedule(queryClient, activeBusinessId);
      const merged: SchedulePayload =
        owns === 'schedule'
          ? { schedule: data.schedule, specialDates: cached.specialDates ?? data.specialDates }
          : { schedule: cached.schedule ?? data.schedule, specialDates: data.specialDates };
      return bizApi(activeBusinessId).put(BIZ_API_PATHS.BUSINESS.SCHEDULE, merged);
    },
    onSuccess: () => {
      toast.success(successMsg);
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_PROFILE(activeBusinessId) });
    },
    onError: () => toast.error(errorMsg),
  });
}

// ─── Hours ────────────────────────────────────────────────────────────────

interface HoursFormProps {
  initialSchedule?: ScheduleDay[];
  initialSpecialDates?: SpecialDate[];
}

export function HoursForm({ initialSchedule, initialSpecialDates }: HoursFormProps) {
  const tSchedule = useTranslations('business.scheduleForm');
  const dayLabels = useDayLabels();
  const { schedule, setSchedule, specialDates } = useSchedule(initialSchedule, initialSpecialDates);
  const mutation = useScheduleMutation('schedule', tSchedule('hoursSaved'), tSchedule('saveError'));
  const editPerm = usePermission('business.update');
  const canEdit = editPerm.allowed;

  function updateDay(index: number, updates: Partial<ScheduleDay>) {
    setSchedule((prev) => prev.map((d, i) => (i === index ? { ...d, ...updates } : d)));
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2">
        {schedule.map((day, index) => (
          <DayRow
            key={day.day}
            label={dayLabels[day.day]}
            day={day}
            onChange={(updates) => updateDay(index, updates)}
          />
        ))}
      </div>
      {editPerm.isError && <PermissionLoadError onRetry={editPerm.refetch} />}

      <div className="flex items-center justify-end pt-1">
        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={() => mutation.mutate({ schedule, specialDates })}
          disabled={mutation.isPending || !canEdit}
        >
          {mutation.isPending ? tSchedule('saving') : tSchedule('saveHours')}
        </Button>
      </div>
    </div>
  );
}

function DayRow({
  label,
  day,
  onChange,
}: {
  label: string;
  day: ScheduleDay;
  onChange: (updates: Partial<ScheduleDay>) => void;
}) {
  const tSchedule = useTranslations('business.scheduleForm');
  const open = !day.closed;
  return (
    <div className="grid grid-cols-[120px_1fr] items-center gap-4 rounded-md border border-line-soft bg-paper px-4 py-2.5 sm:grid-cols-[120px_140px_1fr]">
      <span className="text-sm font-medium text-ink">{label}</span>
      <div className="flex items-center gap-2">
        <Switch
          checked={open}
          onCheckedChange={(checked) => onChange({ closed: !checked })}
          aria-label={`${label} — ${tSchedule('open')}`}
        />
        <span className="text-xs text-ink-mid">
          {open ? tSchedule('open') : tSchedule('closedShort')}
        </span>
      </div>
      <div
        className={`flex items-center gap-2 transition-opacity ${open ? 'opacity-100' : 'opacity-40'}`}
        aria-hidden={!open}
      >
        <TimeBox
          value={day.open}
          onChange={(v) => onChange({ open: v })}
          disabled={!open}
          ariaLabel={tSchedule('openAria', { label })}
        />
        <span className="text-ink-soft">—</span>
        <TimeBox
          value={day.close}
          onChange={(v) => onChange({ close: v })}
          disabled={!open}
          ariaLabel={tSchedule('closeAria', { label })}
        />
      </div>
    </div>
  );
}

function TimeBox({
  value,
  onChange,
  disabled,
  ariaLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  ariaLabel?: string;
}) {
  return (
    <input
      type="time"
      aria-label={ariaLabel}
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand disabled:cursor-not-allowed disabled:opacity-60"
      style={{ minWidth: 78 }}
    />
  );
}

// ─── Special dates ────────────────────────────────────────────────────────

interface SpecialDatesFormProps {
  initialSchedule?: ScheduleDay[];
  initialSpecialDates?: SpecialDate[];
}

export function SpecialDatesForm({ initialSchedule, initialSpecialDates }: SpecialDatesFormProps) {
  const tSchedule = useTranslations('business.scheduleForm');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const { schedule, specialDates, setSpecialDates } = useSchedule(
    initialSchedule,
    initialSpecialDates
  );
  const [calendarOpen, setCalendarOpen] = useState(false);
  const mutation = useScheduleMutation(
    'specialDates',
    tSchedule('datesSaved'),
    tSchedule('saveError')
  );
  const editPerm = usePermission('business.update');
  const canEdit = editPerm.allowed;

  function addSpecialDate(date: Date) {
    const iso = format(date, 'yyyy-MM-dd');
    if (specialDates.some((sd) => sd.date === iso)) return;
    setSpecialDates((prev) => [...prev, { date: iso, closed: true }]);
    setCalendarOpen(false);
  }

  function updateSpecialDate(index: number, updates: Partial<SpecialDate>) {
    setSpecialDates((prev) => prev.map((sd, i) => (i === index ? { ...sd, ...updates } : sd)));
  }

  function removeSpecialDate(index: number) {
    setSpecialDates((prev) => prev.filter((_, i) => i !== index));
  }

  return (
    <div className="flex flex-col gap-4">
      {specialDates.length === 0 && (
        <p className="text-sm text-ink-soft">{tSchedule('noSpecialDates')}</p>
      )}

      {specialDates.length > 0 && (
        <div className="flex flex-col gap-2">
          {specialDates.map((sd, index) => (
            <SpecialDateRow
              key={sd.date}
              date={sd}
              onChange={(updates) => updateSpecialDate(index, updates)}
              onRemove={() => removeSpecialDate(index)}
            />
          ))}
        </div>
      )}

      {editPerm.isError && <PermissionLoadError onRetry={editPerm.refetch} />}

      <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
        <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
          <PopoverTrigger asChild>
            <Button type="button" variant="secondary" size="sm" disabled={!canEdit}>
              <CalendarIcon className="mr-1.5 h-4 w-4" aria-hidden />
              {tSchedule('addDate')}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="start">
            <Calendar
              mode="single"
              onSelect={(date) => date && addSpecialDate(date)}
              locale={dateFnsLocale}
            />
          </PopoverContent>
        </Popover>

        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={() => mutation.mutate({ schedule, specialDates })}
          disabled={mutation.isPending || !canEdit}
        >
          {mutation.isPending ? tSchedule('saving') : tSchedule('saveDates')}
        </Button>
      </div>
    </div>
  );
}

function SpecialDateRow({
  date,
  onChange,
  onRemove,
}: {
  date: SpecialDate;
  onChange: (updates: Partial<SpecialDate>) => void;
  onRemove: () => void;
}) {
  const tSchedule = useTranslations('business.scheduleForm');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const closed = date.closed;
  const formatted = format(parseISO(date.date), 'd MMMM · yyyy', { locale: dateFnsLocale });
  return (
    <div className="grid grid-cols-[1fr_auto] items-center gap-3 rounded-md border border-line-soft bg-paper px-4 py-3 sm:grid-cols-[180px_1fr_auto_auto]">
      <MonoLabel tone="ink" className="text-[13px] normal-case tracking-normal">
        {formatted}
      </MonoLabel>

      <div className="flex items-center gap-3">
        {closed ? (
          <Badge tone="warning" dot>
            {tSchedule('closed')}
          </Badge>
        ) : (
          <Badge tone="info" dot>
            {tSchedule('specialMode')}
          </Badge>
        )}
        <Switch
          checked={!closed}
          onCheckedChange={(checked) =>
            onChange({
              closed: !checked,
              open: checked ? (date.open ?? '10:00') : undefined,
              close: checked ? (date.close ?? '18:00') : undefined,
            })
          }
          aria-label={tSchedule('enabledAria', { label: formatted })}
        />
      </div>

      {!closed ? (
        <div className="flex items-center gap-2">
          <input
            type="time"
            aria-label={tSchedule('openAria', { label: formatted })}
            value={date.open ?? '10:00'}
            onChange={(e) => onChange({ open: e.target.value })}
            className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand"
            style={{ minWidth: 78 }}
          />
          <span className="text-ink-soft">—</span>
          <input
            type="time"
            aria-label={tSchedule('closeAria', { label: formatted })}
            value={date.close ?? '18:00'}
            onChange={(e) => onChange({ close: e.target.value })}
            className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand"
            style={{ minWidth: 78 }}
          />
        </div>
      ) : (
        <span className="font-mono text-[13px] text-ink-soft">—</span>
      )}

      <Button
        type="button"
        variant="ghost"
        size="icon"
        onClick={onRemove}
        aria-label={tSchedule('removeAria', { label: formatted })}
      >
        <X className="h-4 w-4" aria-hidden />
      </Button>
    </div>
  );
}

// Backwards-compat shim: the old `<ScheduleForm />` rendered both blocks.
// Kept as a thin wrapper so any straggler import still resolves; the new
// page composes `<HoursForm />` and `<SpecialDatesForm />` directly.
export function ScheduleForm({ initialSchedule, initialSpecialDates }: HoursFormProps) {
  return (
    <div className="flex flex-col gap-8">
      <HoursForm initialSchedule={initialSchedule} initialSpecialDates={initialSpecialDates} />
      <SpecialDatesForm
        initialSchedule={initialSchedule}
        initialSpecialDates={initialSpecialDates}
      />
    </div>
  );
}
