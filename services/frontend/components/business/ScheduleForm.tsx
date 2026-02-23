'use client';

import { useState, useEffect, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { format, parseISO } from 'date-fns';
import { ru } from 'date-fns/locale';
import { CalendarIcon, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Calendar } from '@/components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Separator } from '@/components/ui/separator';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import type { ScheduleDay, SpecialDate } from '@/types/business';

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'));
const MINUTES = ['00', '15', '30', '45'];

function TimeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [hh, mm] = value.split(':');
  return (
    <div className="flex items-center rounded-md border border-input bg-background text-sm">
      <select
        value={hh}
        onChange={(e) => onChange(`${e.target.value}:${mm}`)}
        className="appearance-none bg-transparent px-2 py-1.5 outline-none cursor-pointer"
      >
        {HOURS.map((h) => (
          <option key={h} value={h}>{h}</option>
        ))}
      </select>
      <span className="text-muted-foreground select-none">:</span>
      <select
        value={mm}
        onChange={(e) => onChange(`${hh}:${e.target.value}`)}
        className="appearance-none bg-transparent px-2 py-1.5 outline-none cursor-pointer"
      >
        {MINUTES.map((m) => (
          <option key={m} value={m}>{m}</option>
        ))}
      </select>
    </div>
  );
}

const DAY_LABELS: Record<ScheduleDay['day'], string> = {
  mon: 'Понедельник',
  tue: 'Вторник',
  wed: 'Среда',
  thu: 'Четверг',
  fri: 'Пятница',
  sat: 'Суббота',
  sun: 'Воскресенье',
};

const DEFAULT_SCHEDULE: ScheduleDay[] = [
  { day: 'mon', open: '09:00', close: '21:00', closed: false },
  { day: 'tue', open: '09:00', close: '21:00', closed: false },
  { day: 'wed', open: '09:00', close: '21:00', closed: false },
  { day: 'thu', open: '09:00', close: '21:00', closed: false },
  { day: 'fri', open: '09:00', close: '21:00', closed: false },
  { day: 'sat', open: '09:00', close: '21:00', closed: false },
  { day: 'sun', open: '09:00', close: '21:00', closed: true },
];

interface ScheduleFormProps {
  initialSchedule?: ScheduleDay[];
  initialSpecialDates?: SpecialDate[];
}

export function ScheduleForm({ initialSchedule, initialSpecialDates }: ScheduleFormProps) {
  const [schedule, setSchedule] = useState<ScheduleDay[]>(DEFAULT_SCHEDULE);
  const [specialDates, setSpecialDates] = useState<SpecialDate[]>([]);
  const [calendarOpen, setCalendarOpen] = useState(false);
  const initialized = useRef(false);
  const queryClient = useQueryClient();

  useEffect(() => {
    if (initialSchedule && initialSchedule.length > 0 && !initialized.current) {
      setSchedule(initialSchedule);
      initialized.current = true;
    }
    if (initialSpecialDates && !initialized.current) {
      setSpecialDates(initialSpecialDates);
    }
  }, [initialSchedule, initialSpecialDates]);

  const mutation = useMutation({
    mutationFn: (data: { schedule: ScheduleDay[]; specialDates: SpecialDate[] }) =>
      api.put('/business/schedule', data),
    onSuccess: () => {
      toast.success('Расписание сохранено');
      queryClient.invalidateQueries({ queryKey: ['business'] });
    },
    onError: () => {
      toast.error('Ошибка сохранения');
    },
  });

  function updateDay(index: number, updates: Partial<ScheduleDay>) {
    setSchedule((prev) => prev.map((d, i) => (i === index ? { ...d, ...updates } : d)));
  }

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

  function handleSave() {
    mutation.mutate({ schedule, specialDates });
  }

  return (
    <div className="space-y-6">
      {/* Weekly schedule */}
      <div className="space-y-3">
        {schedule.map((day, index) => (
          <div key={day.day} className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border p-3">
            <span className="w-28 text-sm font-medium">{DAY_LABELS[day.day]}</span>

            <div className="flex items-center gap-2">
              <Switch
                checked={!day.closed}
                onCheckedChange={(checked) => updateDay(index, { closed: !checked })}
              />
              <Label className="text-sm text-muted-foreground">
                {day.closed ? 'Выходной' : 'Открыто'}
              </Label>
            </div>

            {!day.closed && (
              <div className="flex items-center gap-2 sm:ml-0 ml-auto w-full sm:w-auto">
                <TimeSelect value={day.open} onChange={(v) => updateDay(index, { open: v })} />
                <span className="text-muted-foreground">&mdash;</span>
                <TimeSelect value={day.close} onChange={(v) => updateDay(index, { close: v })} />
              </div>
            )}
          </div>
        ))}
      </div>

      <Separator />

      {/* Special dates */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium">Особые даты</h3>
            <p className="text-xs text-muted-foreground">Праздники и особый режим работы</p>
          </div>
          <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm">
                <CalendarIcon className="mr-2 h-4 w-4" />
                Добавить дату
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="end">
              <Calendar
                mode="single"
                onSelect={(date) => date && addSpecialDate(date)}
                locale={ru}
              />
            </PopoverContent>
          </Popover>
        </div>

        {specialDates.length === 0 && (
          <p className="text-sm text-muted-foreground">Нет особых дат</p>
        )}

        {specialDates.map((sd, index) => (
          <div key={sd.date} className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border p-3">
            <span className="w-28 text-sm font-medium">
              {format(parseISO(sd.date), 'd MMMM', { locale: ru })}
            </span>

            <div className="flex items-center gap-2">
              <Switch
                checked={!sd.closed}
                onCheckedChange={(checked) =>
                  updateSpecialDate(index, {
                    closed: !checked,
                    open: checked ? '10:00' : undefined,
                    close: checked ? '18:00' : undefined,
                  })
                }
              />
              <Label className="text-sm text-muted-foreground">
                {sd.closed ? 'Выходной' : 'Особый режим'}
              </Label>
            </div>

            <Button
              variant="ghost"
              size="icon"
              className="ml-auto h-8 w-8 text-muted-foreground hover:text-destructive"
              onClick={() => removeSpecialDate(index)}
            >
              <X className="h-4 w-4" />
            </Button>

            {!sd.closed && (
              <div className="flex items-center gap-2 w-full sm:w-auto">
                <TimeSelect
                  value={sd.open || '00:00'}
                  onChange={(v) => updateSpecialDate(index, { open: v })}
                />
                <span className="text-muted-foreground">&mdash;</span>
                <TimeSelect
                  value={sd.close || '00:00'}
                  onChange={(v) => updateSpecialDate(index, { close: v })}
                />
              </div>
            )}
          </div>
        ))}
      </div>

      <Button onClick={handleSave} disabled={mutation.isPending}>
        {mutation.isPending ? 'Сохранение...' : 'Сохранить расписание'}
      </Button>
    </div>
  );
}
