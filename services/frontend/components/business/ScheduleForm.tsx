'use client';

import { useState, useEffect, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import type { ScheduleDay } from '@/types/business';

const DAYS: { key: ScheduleDay['day']; label: string }[] = [
  { key: 'mon', label: 'Пн' },
  { key: 'tue', label: 'Вт' },
  { key: 'wed', label: 'Ср' },
  { key: 'thu', label: 'Чт' },
  { key: 'fri', label: 'Пт' },
  { key: 'sat', label: 'Сб' },
  { key: 'sun', label: 'Вс' },
];

const defaultSchedule: ScheduleDay[] = DAYS.map(({ key }) => ({
  day: key,
  open: '09:00',
  close: '21:00',
  closed: key === 'sun',
}));

export function ScheduleForm({ initialSchedule }: { initialSchedule?: ScheduleDay[] }) {
  const [schedule, setSchedule] = useState<ScheduleDay[]>(initialSchedule ?? defaultSchedule);
  const qc = useQueryClient();
  const initialized = useRef(false);

  useEffect(() => {
    if (!initialized.current && initialSchedule && initialSchedule.length > 0) {
      setSchedule(initialSchedule);
      initialized.current = true;
    }
  }, [initialSchedule]);

  const mutation = useMutation({
    mutationFn: () => api.put('/business/schedule', { schedule }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['business'] });
      toast.success('Расписание сохранено');
    },
    onError: () => toast.error('Ошибка сохранения'),
  });

  const update = (day: ScheduleDay['day'], patch: Partial<ScheduleDay>) => {
    setSchedule((s) => s.map((d) => (d.day === day ? { ...d, ...patch } : d)));
  };

  return (
    <div className="space-y-3">
      <div className="grid gap-2">
        {DAYS.map(({ key, label }) => {
          const day = schedule.find((d) => d.day === key);
          if (!day) return null;
          return (
            <div key={key} className="flex items-center gap-3">
              <span className="w-8 text-sm font-medium text-gray-600">{label}</span>
              <label className="flex cursor-pointer items-center gap-1 text-sm text-gray-500">
                <input
                  type="checkbox"
                  checked={day.closed}
                  onChange={(e) => update(key, { closed: e.target.checked })}
                />
                Выходной
              </label>
              {!day.closed && (
                <>
                  <Input
                    type="time"
                    value={day.open}
                    onChange={(e) => update(key, { open: e.target.value })}
                    className="w-28"
                  />
                  <span className="text-gray-400">—</span>
                  <Input
                    type="time"
                    value={day.close}
                    onChange={(e) => update(key, { close: e.target.value })}
                    className="w-28"
                  />
                </>
              )}
            </div>
          );
        })}
      </div>
      <Button onClick={() => mutation.mutate()} disabled={mutation.isPending} type="button">
        {mutation.isPending ? 'Сохранение...' : 'Сохранить расписание'}
      </Button>
    </div>
  );
}
