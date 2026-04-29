'use client';

// Linen rebuild — Phase 4.8.
// Splits the previous single block into two API-aware sections:
//   <HoursForm />          — weekly hours (Часы работы)
//   <SpecialDatesForm />   — date overrides (Особые даты)
// Both share the same PUT /business/schedule mutation: backend stores the
// `{schedule, specialDates}` payload under business.settings.schedule, so
// each section sends its slice alongside the other section's current value
// to avoid clobbering. Parent owns the source of truth.

import { useState, useEffect, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { format, parseISO } from 'date-fns';
import { ru } from 'date-fns/locale';
import { CalendarIcon, X } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { Calendar } from '@/components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { MonoLabel } from '@/components/ui/mono-label';
import { Badge } from '@/components/ui/badge';
import type { ScheduleDay, SpecialDate } from '@/types/business';

const DEFAULT_SCHEDULE: ScheduleDay[] = [
  { day: 'mon', open: '09:00', close: '21:00', closed: false },
  { day: 'tue', open: '09:00', close: '21:00', closed: false },
  { day: 'wed', open: '09:00', close: '21:00', closed: false },
  { day: 'thu', open: '09:00', close: '21:00', closed: false },
  { day: 'fri', open: '09:00', close: '21:00', closed: false },
  { day: 'sat', open: '10:00', close: '21:00', closed: false },
  { day: 'sun', open: '10:00', close: '20:00', closed: true },
];

const DAY_LABELS: Record<ScheduleDay['day'], string> = {
  mon: 'Понедельник',
  tue: 'Вторник',
  wed: 'Среда',
  thu: 'Четверг',
  fri: 'Пятница',
  sat: 'Суббота',
  sun: 'Воскресенье',
};

interface SchedulePayload {
  schedule: ScheduleDay[];
  specialDates: SpecialDate[];
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

function useScheduleMutation(label: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: SchedulePayload) => api.put('/business/schedule', data),
    onSuccess: () => {
      toast.success(`${label} сохранены`);
      queryClient.invalidateQueries({ queryKey: ['business'] });
    },
    onError: () => toast.error('Не получилось сохранить'),
  });
}

// ─── Hours ────────────────────────────────────────────────────────────────

interface HoursFormProps {
  initialSchedule?: ScheduleDay[];
  initialSpecialDates?: SpecialDate[];
}

export function HoursForm({ initialSchedule, initialSpecialDates }: HoursFormProps) {
  const { schedule, setSchedule, specialDates } = useSchedule(initialSchedule, initialSpecialDates);
  const mutation = useScheduleMutation('Часы работы');

  function updateDay(index: number, updates: Partial<ScheduleDay>) {
    setSchedule((prev) => prev.map((d, i) => (i === index ? { ...d, ...updates } : d)));
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2">
        {schedule.map((day, index) => (
          <DayRow
            key={day.day}
            label={DAY_LABELS[day.day]}
            day={day}
            onChange={(updates) => updateDay(index, updates)}
          />
        ))}
      </div>
      <div className="flex items-center justify-end pt-1">
        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={() => mutation.mutate({ schedule, specialDates })}
          disabled={mutation.isPending}
        >
          {mutation.isPending ? 'Сохраняем…' : 'Сохранить часы'}
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
  const open = !day.closed;
  return (
    <div className="grid grid-cols-[120px_1fr] items-center gap-4 rounded-md border border-line-soft bg-paper px-4 py-2.5 sm:grid-cols-[120px_140px_1fr]">
      <span className="text-sm font-medium text-ink">{label}</span>
      <div className="flex items-center gap-2">
        <Switch
          checked={open}
          onCheckedChange={(checked) => onChange({ closed: !checked })}
          aria-label={`${label} — открыто`}
        />
        <span className="text-xs text-ink-mid">{open ? 'открыто' : 'закрыто'}</span>
      </div>
      <div
        className={`flex items-center gap-2 transition-opacity ${open ? 'opacity-100' : 'opacity-40'}`}
        aria-hidden={!open}
      >
        <TimeBox
          value={day.open}
          onChange={(v) => onChange({ open: v })}
          disabled={!open}
          ariaLabel={`${label} — открытие`}
        />
        <span className="text-ink-soft">—</span>
        <TimeBox
          value={day.close}
          onChange={(v) => onChange({ close: v })}
          disabled={!open}
          ariaLabel={`${label} — закрытие`}
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
      className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-ochre focus:outline-none focus:ring-2 focus:ring-ochre/20 disabled:cursor-not-allowed disabled:opacity-60"
      style={{ minWidth: 78 }}
    />
  );
}

// ─── Special dates ────────────────────────────────────────────────────────

interface SpecialDatesFormProps {
  initialSchedule?: ScheduleDay[];
  initialSpecialDates?: SpecialDate[];
}

export function SpecialDatesForm({
  initialSchedule,
  initialSpecialDates,
}: SpecialDatesFormProps) {
  const { schedule, specialDates, setSpecialDates } = useSchedule(
    initialSchedule,
    initialSpecialDates
  );
  const [calendarOpen, setCalendarOpen] = useState(false);
  const mutation = useScheduleMutation('Особые даты');

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
        <p className="text-sm text-ink-soft">
          Пока ничего не отмечено. Добавьте даты, которые ИИ должен учесть — праздники, ремонты,
          корпоративы.
        </p>
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

      <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
        <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
          <PopoverTrigger asChild>
            <Button type="button" variant="secondary" size="sm">
              <CalendarIcon className="mr-1.5 h-4 w-4" aria-hidden />
              Добавить дату
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="start">
            <Calendar
              mode="single"
              onSelect={(date) => date && addSpecialDate(date)}
              locale={ru}
            />
          </PopoverContent>
        </Popover>

        <Button
          type="button"
          variant="primary"
          size="md"
          onClick={() => mutation.mutate({ schedule, specialDates })}
          disabled={mutation.isPending}
        >
          {mutation.isPending ? 'Сохраняем…' : 'Сохранить даты'}
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
  const closed = date.closed;
  const formatted = format(parseISO(date.date), 'd MMMM · yyyy', { locale: ru });
  return (
    <div className="grid grid-cols-[1fr_auto] items-center gap-3 rounded-md border border-line-soft bg-paper px-4 py-3 sm:grid-cols-[180px_1fr_auto_auto]">
      <MonoLabel tone="ink" className="text-[13px] normal-case tracking-normal">
        {formatted}
      </MonoLabel>

      <div className="flex items-center gap-3">
        {closed ? (
          <Badge tone="warning" dot>
            Закрыто
          </Badge>
        ) : (
          <Badge tone="info" dot>
            Особый режим
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
          aria-label={`${formatted} — открыто`}
        />
      </div>

      {!closed ? (
        <div className="flex items-center gap-2">
          <input
            type="time"
            aria-label={`${formatted} — открытие`}
            value={date.open ?? '10:00'}
            onChange={(e) => onChange({ open: e.target.value })}
            className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-ochre focus:outline-none focus:ring-2 focus:ring-ochre/20"
            style={{ minWidth: 78 }}
          />
          <span className="text-ink-soft">—</span>
          <input
            type="time"
            aria-label={`${formatted} — закрытие`}
            value={date.close ?? '18:00'}
            onChange={(e) => onChange({ close: e.target.value })}
            className="h-8 rounded-sm border border-line bg-paper-raised px-2 text-center font-mono text-[13px] text-ink focus:border-ochre focus:outline-none focus:ring-2 focus:ring-ochre/20"
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
        aria-label={`Удалить ${formatted}`}
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
