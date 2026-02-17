'use client'

import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ProfileForm } from '@/components/business/ProfileForm'
import { ScheduleForm } from '@/components/business/ScheduleForm'
import { Separator } from '@/components/ui/separator'
import type { Business } from '@/types/business'

export default function BusinessPage() {
  const { data, isLoading } = useQuery<Business>({
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data.business as Business),
  })

  if (isLoading) return <div className="p-8 text-gray-400">Загрузка...</div>

  return (
    <div className="p-8 max-w-2xl space-y-8">
      <div>
        <h1 className="text-2xl font-bold mb-1">Профиль бизнеса</h1>
        <p className="text-gray-500 text-sm">Эта информация используется ИИ при общении с клиентами</p>
      </div>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Основная информация</h2>
        <ProfileForm defaultValues={data} />
      </section>

      <Separator />

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Расписание работы</h2>
        <ScheduleForm initialSchedule={data?.schedule} />
      </section>
    </div>
  )
}
