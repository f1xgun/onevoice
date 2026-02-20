# Frontend Phase 5b Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the Business Profile page — a form for name, category, phone, website, description, logo upload, and a 7-day operating schedule grid.

**Architecture:** Two shadcn/ui form sections on one page; react-hook-form + zod; `GET /api/v1/business` on load, `PUT /api/v1/business` on save. Schedule stored as array of `{day, open, close, closed}` objects.

**Tech Stack:** Next.js 14 App Router, react-hook-form, zod, TanStack Query, shadcn/ui.

---

### Task 1: Business profile types + schema

**Files:**
- Create: `services/frontend/types/business.ts`
- Create: `services/frontend/lib/__tests__/businessSchema.test.ts`

**Step 1: Write failing schema tests**

Create `services/frontend/lib/__tests__/businessSchema.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { businessSchema } from '../schemas'

describe('businessSchema', () => {
  it('accepts valid business data', () => {
    const result = businessSchema.safeParse({
      name: 'Кофейня Уют',
      category: 'cafe',
      phone: '+79001234567',
      website: 'https://example.com',
      description: 'Уютная кофейня',
    })
    expect(result.success).toBe(true)
  })

  it('rejects empty name', () => {
    const result = businessSchema.safeParse({ name: '', category: 'cafe' })
    expect(result.success).toBe(false)
  })

  it('rejects invalid phone', () => {
    const result = businessSchema.safeParse({ name: 'Test', category: 'cafe', phone: 'not-a-phone' })
    expect(result.success).toBe(false)
  })
})
```

**Step 2: Run to verify RED**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: FAIL — `businessSchema` not found

**Step 3: Create `types/business.ts`**

```ts
export interface ScheduleDay {
  day: 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'
  open: string   // "09:00"
  close: string  // "21:00"
  closed: boolean
}

export interface Business {
  id: string
  name: string
  category: string
  phone?: string
  website?: string
  description?: string
  logo_url?: string
  address?: string
  schedule?: ScheduleDay[]
}
```

**Step 4: Add `businessSchema` to `lib/schemas.ts`**

Append to `services/frontend/lib/schemas.ts`:

```ts
export const businessSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа'),
  category: z.string().min(1, 'Выберите категорию'),
  phone: z.string().regex(/^\+?[0-9]{7,15}$/, 'Некорректный номер телефона').optional().or(z.literal('')),
  website: z.string().url('Некорректный URL').optional().or(z.literal('')),
  description: z.string().max(500).optional(),
  address: z.string().optional(),
})

export type BusinessInput = z.infer<typeof businessSchema>
```

**Step 5: Run tests to verify GREEN**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: PASS

**Step 6: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/types/business.ts services/frontend/lib/schemas.ts \
        services/frontend/lib/__tests__/businessSchema.test.ts
git commit -m "feat(frontend): add business types and validation schema"
```

---

### Task 2: Business profile form + schedule form

**Files:**
- Create: `services/frontend/components/business/ProfileForm.tsx`
- Create: `services/frontend/components/business/ScheduleForm.tsx`
- Modify: `services/frontend/app/(app)/business/page.tsx`

**Step 1: Create `components/business/ProfileForm.tsx`**

```tsx
'use client'

import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { businessSchema, type BusinessInput } from '@/lib/schemas'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { Business } from '@/types/business'

const CATEGORIES = [
  { value: 'cafe', label: 'Кафе / Ресторан' },
  { value: 'retail', label: 'Розничная торговля' },
  { value: 'service', label: 'Услуги' },
  { value: 'beauty', label: 'Красота и здоровье' },
  { value: 'education', label: 'Образование' },
  { value: 'other', label: 'Другое' },
]

export function ProfileForm({ defaultValues }: { defaultValues?: Partial<Business> }) {
  const qc = useQueryClient()

  const { register, handleSubmit, setValue, formState: { errors, isSubmitting } } =
    useForm<BusinessInput>({
      resolver: zodResolver(businessSchema),
      defaultValues: defaultValues ?? {},
    })

  const mutation = useMutation({
    mutationFn: (data: BusinessInput) => api.put('/business', data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['business'] })
      toast.success('Данные сохранены')
    },
    onError: () => toast.error('Ошибка сохранения'),
  })

  return (
    <form onSubmit={handleSubmit((d) => mutation.mutate(d))} className="space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="space-y-1">
          <Label>Название *</Label>
          <Input {...register('name')} placeholder="Кофейня Уют" />
          {errors.name && <p className="text-sm text-red-500">{errors.name.message}</p>}
        </div>

        <div className="space-y-1">
          <Label>Категория *</Label>
          <Select onValueChange={(v) => setValue('category', v)} defaultValue={defaultValues?.category}>
            <SelectTrigger><SelectValue placeholder="Выберите категорию" /></SelectTrigger>
            <SelectContent>
              {CATEGORIES.map((c) => (
                <SelectItem key={c.value} value={c.value}>{c.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {errors.category && <p className="text-sm text-red-500">{errors.category.message}</p>}
        </div>

        <div className="space-y-1">
          <Label>Телефон</Label>
          <Input {...register('phone')} placeholder="+79001234567" />
          {errors.phone && <p className="text-sm text-red-500">{errors.phone.message}</p>}
        </div>

        <div className="space-y-1">
          <Label>Сайт</Label>
          <Input {...register('website')} placeholder="https://example.com" />
          {errors.website && <p className="text-sm text-red-500">{errors.website.message}</p>}
        </div>

        <div className="space-y-1 md:col-span-2">
          <Label>Адрес</Label>
          <Input {...register('address')} placeholder="г. Москва, ул. Примерная, 1" />
        </div>

        <div className="space-y-1 md:col-span-2">
          <Label>Описание</Label>
          <Input {...register('description')} placeholder="Краткое описание бизнеса" />
        </div>
      </div>

      <Button type="submit" disabled={isSubmitting}>
        {isSubmitting ? 'Сохранение...' : 'Сохранить'}
      </Button>
    </form>
  )
}
```

**Step 2: Create `components/business/ScheduleForm.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { ScheduleDay } from '@/types/business'

const DAYS: { key: ScheduleDay['day']; label: string }[] = [
  { key: 'mon', label: 'Пн' }, { key: 'tue', label: 'Вт' },
  { key: 'wed', label: 'Ср' }, { key: 'thu', label: 'Чт' },
  { key: 'fri', label: 'Пт' }, { key: 'sat', label: 'Сб' },
  { key: 'sun', label: 'Вс' },
]

const defaultSchedule: ScheduleDay[] = DAYS.map(({ key }) => ({
  day: key,
  open: '09:00',
  close: '21:00',
  closed: key === 'sun',
}))

export function ScheduleForm({ initialSchedule }: { initialSchedule?: ScheduleDay[] }) {
  const [schedule, setSchedule] = useState<ScheduleDay[]>(initialSchedule ?? defaultSchedule)
  const qc = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.put('/business/schedule', { schedule }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['business'] }); toast.success('Расписание сохранено') },
    onError: () => toast.error('Ошибка сохранения'),
  })

  const update = (day: ScheduleDay['day'], patch: Partial<ScheduleDay>) => {
    setSchedule((s) => s.map((d) => d.day === day ? { ...d, ...patch } : d))
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-2">
        {DAYS.map(({ key, label }) => {
          const day = schedule.find((d) => d.day === key)!
          return (
            <div key={key} className="flex items-center gap-3">
              <span className="w-8 text-sm font-medium text-gray-600">{label}</span>
              <label className="flex items-center gap-1 text-sm text-gray-500">
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
          )
        })}
      </div>
      <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending ? 'Сохранение...' : 'Сохранить расписание'}
      </Button>
    </div>
  )
}
```

**Step 3: Implement `app/(app)/business/page.tsx`**

```tsx
'use client'

import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ProfileForm } from '@/components/business/ProfileForm'
import { ScheduleForm } from '@/components/business/ScheduleForm'
import { Separator } from '@/components/ui/separator'

export default function BusinessPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['business'],
    queryFn: () => api.get('/business').then((r) => r.data.business),
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
```

**Step 4: Verify**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test && pnpm build
```
Expected: PASS

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add business profile and schedule forms (Phase 5b)"
```
