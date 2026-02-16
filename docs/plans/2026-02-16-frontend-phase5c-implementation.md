# Frontend Phase 5c Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the landing page (static, SEO, Russian copy) plus the remaining app pages: reviews feed with AI reply dialog, posts history table, tasks log table, and account settings form.

**Architecture:** Landing page is a Server Component (RSC, no `"use client"`) for SEO. App pages use TanStack Query + shadcn DataTable. Review reply uses a mutation to generate AI text then publish.

**Tech Stack:** Next.js 14 App Router (RSC for landing), TanStack Query, shadcn/ui DataTable (via `@tanstack/react-table`).

---

### Task 1: Landing page

**Files:**
- Create: `services/frontend/app/(public)/page.tsx`
- Create: `services/frontend/components/landing/Hero.tsx`
- Create: `services/frontend/components/landing/Features.tsx`
- Create: `services/frontend/components/landing/Platforms.tsx`
- Create: `services/frontend/components/landing/CTABanner.tsx`
- Create: `services/frontend/components/landing/Footer.tsx`

**Step 1: Install missing dep**

```bash
cd /Users/f1xgun/onevoice/services/frontend
pnpm add @tanstack/react-table
pnpm dlx shadcn-ui@latest add table
```

**Step 2: Create `components/landing/Hero.tsx`** (Server Component)

```tsx
import Link from 'next/link'

export function Hero() {
  return (
    <section className="min-h-screen flex flex-col items-center justify-center bg-gradient-to-br from-gray-900 to-gray-800 text-white text-center px-6">
      <h1 className="text-5xl md:text-6xl font-bold mb-6 max-w-3xl leading-tight">
        Управляйте цифровым<br />присутствием с помощью ИИ
      </h1>
      <p className="text-xl text-gray-300 mb-10 max-w-xl">
        OneVoice автоматизирует публикации, отзывы и информацию о вашем бизнесе
        в VK, Telegram и Яндекс.Бизнес — одним сообщением.
      </p>
      <div className="flex gap-4 flex-wrap justify-center">
        <Link
          href="/register"
          className="px-8 py-3 bg-blue-600 hover:bg-blue-700 rounded-lg font-semibold transition-colors"
        >
          Попробовать бесплатно
        </Link>
        <Link
          href="/login"
          className="px-8 py-3 border border-gray-500 hover:border-gray-300 rounded-lg font-semibold transition-colors"
        >
          Войти
        </Link>
      </div>
    </section>
  )
}
```

**Step 3: Create `components/landing/Features.tsx`**

```tsx
const FEATURES = [
  {
    icon: '💬',
    title: 'Один чат — все платформы',
    description: 'Напишите одно сообщение — ИИ опубликует во всех подключённых сетях одновременно.',
  },
  {
    icon: '⭐',
    title: 'Автоответы на отзывы',
    description: 'Система мониторит отзывы и предлагает персонализированные ответы в вашем стиле.',
  },
  {
    icon: '🕐',
    title: 'Актуальная информация',
    description: 'Обновите часы работы или контакты — изменения применятся на всех площадках.',
  },
]

export function Features() {
  return (
    <section className="py-24 bg-white px-6">
      <div className="max-w-5xl mx-auto">
        <h2 className="text-3xl font-bold text-center mb-4">Что умеет OneVoice</h2>
        <p className="text-gray-500 text-center mb-12">Экономьте часы ежедневной работы</p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
          {FEATURES.map((f) => (
            <div key={f.title} className="p-6 rounded-xl border border-gray-100 hover:shadow-md transition-shadow">
              <div className="text-4xl mb-4">{f.icon}</div>
              <h3 className="text-lg font-semibold mb-2">{f.title}</h3>
              <p className="text-gray-500 text-sm">{f.description}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
```

**Step 4: Create `components/landing/Platforms.tsx`**

```tsx
const PLATFORMS = [
  { name: 'ВКонтакте', color: '#4680C2' },
  { name: 'Telegram', color: '#2AABEE' },
  { name: 'Яндекс.Бизнес', color: '#FC3F1D' },
]

export function Platforms() {
  return (
    <section className="py-16 bg-gray-50 px-6 text-center">
      <h2 className="text-2xl font-bold mb-8">Поддерживаемые платформы</h2>
      <div className="flex justify-center gap-6 flex-wrap">
        {PLATFORMS.map((p) => (
          <div
            key={p.name}
            className="px-6 py-3 rounded-full text-white font-semibold text-sm"
            style={{ backgroundColor: p.color }}
          >
            {p.name}
          </div>
        ))}
      </div>
    </section>
  )
}
```

**Step 5: Create `components/landing/CTABanner.tsx`**

```tsx
import Link from 'next/link'

export function CTABanner() {
  return (
    <section className="py-24 bg-blue-600 text-white text-center px-6">
      <h2 className="text-3xl font-bold mb-4">Готовы попробовать?</h2>
      <p className="text-blue-100 mb-8">Регистрация занимает меньше минуты</p>
      <Link
        href="/register"
        className="px-8 py-3 bg-white text-blue-600 rounded-lg font-semibold hover:bg-blue-50 transition-colors"
      >
        Начать бесплатно
      </Link>
    </section>
  )
}
```

**Step 6: Create `components/landing/Footer.tsx`**

```tsx
export function Footer() {
  return (
    <footer className="py-8 bg-gray-900 text-gray-400 text-center text-sm">
      <p>© 2026 OneVoice. Все права защищены.</p>
    </footer>
  )
}
```

**Step 7: Create `app/(public)/page.tsx`** (Server Component, no `"use client"`)

```tsx
import { Hero } from '@/components/landing/Hero'
import { Features } from '@/components/landing/Features'
import { Platforms } from '@/components/landing/Platforms'
import { CTABanner } from '@/components/landing/CTABanner'
import { Footer } from '@/components/landing/Footer'

export default function LandingPage() {
  return (
    <main>
      <Hero />
      <Features />
      <Platforms />
      <CTABanner />
      <Footer />
    </main>
  )
}
```

**Step 8: Verify build**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm build
```
Expected: success

**Step 9: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add landing page with hero, features, platforms, CTA sections"
```

---

### Task 2: Reviews feed with AI reply dialog

**Files:**
- Modify: `services/frontend/app/(app)/reviews/page.tsx`
- Create: `services/frontend/components/reviews/ReviewCard.tsx`
- Create: `services/frontend/components/reviews/ReplyDialog.tsx`

**Step 1: Create `components/reviews/ReplyDialog.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

interface Review {
  id: string
  platform: string
  author: string
  rating: number
  text: string
}

interface Props {
  review: Review | null
  open: boolean
  onClose: () => void
}

export function ReplyDialog({ review, open, onClose }: Props) {
  const [replyText, setReplyText] = useState('')

  const generateMutation = useMutation({
    mutationFn: () => api.post(`/reviews/${review?.id}/generate-reply`).then((r) => r.data.reply),
    onSuccess: (text: string) => setReplyText(text),
    onError: () => toast.error('Ошибка генерации ответа'),
  })

  const publishMutation = useMutation({
    mutationFn: () => api.post(`/reviews/${review?.id}/reply`, { text: replyText }),
    onSuccess: () => { toast.success('Ответ опубликован'); onClose() },
    onError: () => toast.error('Ошибка публикации'),
  })

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Ответить на отзыв</DialogTitle>
        </DialogHeader>
        {review && (
          <div className="space-y-4">
            <div className="p-3 bg-gray-50 rounded text-sm text-gray-600 border">
              <p className="font-medium">{review.author} — {'★'.repeat(review.rating)}</p>
              <p className="mt-1">{review.text}</p>
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Ваш ответ</Label>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => generateMutation.mutate()}
                  disabled={generateMutation.isPending}
                >
                  {generateMutation.isPending ? 'Генерация...' : '✨ Ответить с ИИ'}
                </Button>
              </div>
              <textarea
                className="w-full border rounded p-2 text-sm h-32 resize-none focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="Напишите ответ или сгенерируйте с ИИ..."
                value={replyText}
                onChange={(e) => setReplyText(e.target.value)}
              />
            </div>
            <div className="flex gap-2 justify-end">
              <Button variant="outline" onClick={onClose}>Отмена</Button>
              <Button
                onClick={() => publishMutation.mutate()}
                disabled={!replyText || publishMutation.isPending}
              >
                {publishMutation.isPending ? 'Публикация...' : 'Опубликовать'}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
```

**Step 2: Create `components/reviews/ReviewCard.tsx`**

```tsx
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

const platformColors: Record<string, string> = {
  vk: '#4680C2', telegram: '#2AABEE', yandex_business: '#FC3F1D',
}
const platformLabels: Record<string, string> = {
  vk: 'ВКонтакте', telegram: 'Telegram', yandex_business: 'Яндекс.Бизнес',
}

interface Props {
  review: { id: string; platform: string; author: string; rating: number; text: string; replied: boolean; created_at: string }
  onReply: () => void
}

export function ReviewCard({ review, onReply }: Props) {
  const color = platformColors[review.platform] ?? '#6b7280'
  const label = platformLabels[review.platform] ?? review.platform

  return (
    <Card>
      <CardContent className="p-4 space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="px-2 py-0.5 rounded text-white text-xs font-bold" style={{ backgroundColor: color }}>
              {label}
            </span>
            <span className="font-medium text-sm">{review.author}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-yellow-400">{'★'.repeat(review.rating)}{'☆'.repeat(5 - review.rating)}</span>
            <span className="text-xs text-gray-400">{new Date(review.created_at).toLocaleDateString('ru')}</span>
          </div>
        </div>
        <p className="text-sm text-gray-700">{review.text}</p>
        <div className="flex items-center justify-between">
          {review.replied
            ? <Badge variant="secondary">Ответ отправлен</Badge>
            : <Badge variant="outline">Без ответа</Badge>}
          {!review.replied && (
            <Button size="sm" variant="outline" onClick={onReply}>Ответить</Button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
```

**Step 3: Implement `app/(app)/reviews/page.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ReviewCard } from '@/components/reviews/ReviewCard'
import { ReplyDialog } from '@/components/reviews/ReplyDialog'

export default function ReviewsPage() {
  const [selectedReview, setSelectedReview] = useState<null | { id: string; platform: string; author: string; rating: number; text: string }>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['reviews'],
    queryFn: () => api.get('/reviews').then((r) => r.data.reviews ?? []),
  })

  if (isLoading) return <div className="p-8 text-gray-400">Загрузка...</div>

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-bold mb-6">Отзывы</h1>
      <div className="space-y-4">
        {data?.length === 0 && <p className="text-gray-400">Нет отзывов. Подключите платформы.</p>}
        {data?.map((review: { id: string; platform: string; author: string; rating: number; text: string; replied: boolean; created_at: string }) => (
          <ReviewCard key={review.id} review={review} onReply={() => setSelectedReview(review)} />
        ))}
      </div>
      <ReplyDialog review={selectedReview} open={!!selectedReview} onClose={() => setSelectedReview(null)} />
    </div>
  )
}
```

**Step 4: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add reviews feed with AI reply dialog"
```

---

### Task 3: Posts, Tasks, Settings pages

**Files:**
- Modify: `services/frontend/app/(app)/posts/page.tsx`
- Modify: `services/frontend/app/(app)/tasks/page.tsx`
- Modify: `services/frontend/app/(app)/settings/page.tsx`

**Step 1: Implement `app/(app)/posts/page.tsx`**

```tsx
'use client'

import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'

const platformLabels: Record<string, string> = {
  vk: 'ВКонтакте', telegram: 'Telegram', yandex_business: 'Яндекс.Бизнес',
}

const statusLabels: Record<string, string> = {
  published: 'Опубликовано', scheduled: 'Запланировано',
  draft: 'Черновик', error: 'Ошибка',
}

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  published: 'default', scheduled: 'secondary', draft: 'outline', error: 'destructive',
}

export default function PostsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['posts'],
    queryFn: () => api.get('/posts').then((r) => r.data.posts ?? []),
  })

  if (isLoading) return <div className="p-8 text-gray-400">Загрузка...</div>

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold mb-6">История публикаций</h1>
      <div className="border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-gray-50 border-b">
            <tr>
              <th className="text-left p-3 text-gray-600">Текст</th>
              <th className="text-left p-3 text-gray-600">Платформа</th>
              <th className="text-left p-3 text-gray-600">Статус</th>
              <th className="text-left p-3 text-gray-600">Дата</th>
            </tr>
          </thead>
          <tbody>
            {data?.length === 0 && (
              <tr><td colSpan={4} className="p-4 text-center text-gray-400">Нет публикаций</td></tr>
            )}
            {data?.map((post: { id: string; text: string; platform: string; status: string; created_at: string }) => (
              <tr key={post.id} className="border-b hover:bg-gray-50">
                <td className="p-3 max-w-xs truncate">{post.text}</td>
                <td className="p-3">{platformLabels[post.platform] ?? post.platform}</td>
                <td className="p-3"><Badge variant={statusVariants[post.status] ?? 'outline'}>{statusLabels[post.status] ?? post.status}</Badge></td>
                <td className="p-3 text-gray-500">{new Date(post.created_at).toLocaleDateString('ru')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

**Step 2: Implement `app/(app)/tasks/page.tsx`**

```tsx
'use client'

import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  completed: 'default', running: 'secondary', failed: 'destructive', pending: 'outline',
}

const statusLabels: Record<string, string> = {
  completed: 'Выполнено', running: 'Выполняется', failed: 'Ошибка', pending: 'Ожидает',
}

export default function TasksPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['tasks'],
    queryFn: () => api.get('/tasks').then((r) => r.data.tasks ?? []),
  })

  if (isLoading) return <div className="p-8 text-gray-400">Загрузка...</div>

  return (
    <div className="p-8">
      <h1 className="text-2xl font-bold mb-6">Задачи агентов</h1>
      <div className="border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-gray-50 border-b">
            <tr>
              <th className="text-left p-3 text-gray-600">Задача</th>
              <th className="text-left p-3 text-gray-600">Агент</th>
              <th className="text-left p-3 text-gray-600">Статус</th>
              <th className="text-left p-3 text-gray-600">Создана</th>
            </tr>
          </thead>
          <tbody>
            {data?.length === 0 && (
              <tr><td colSpan={4} className="p-4 text-center text-gray-400">Нет задач</td></tr>
            )}
            {data?.map((task: { id: string; description: string; agent: string; status: string; created_at: string }) => (
              <tr key={task.id} className="border-b hover:bg-gray-50">
                <td className="p-3 max-w-xs truncate">{task.description}</td>
                <td className="p-3 font-mono text-xs text-gray-600">{task.agent}</td>
                <td className="p-3"><Badge variant={statusVariants[task.status] ?? 'outline'}>{statusLabels[task.status] ?? task.status}</Badge></td>
                <td className="p-3 text-gray-500">{new Date(task.created_at).toLocaleString('ru')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

**Step 3: Implement `app/(app)/settings/page.tsx`**

```tsx
'use client'

import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuthStore } from '@/lib/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'

const profileSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа'),
  email: z.string().email('Некорректный email'),
})

const passwordSchema = z.object({
  currentPassword: z.string().min(6),
  newPassword: z.string().min(6, 'Минимум 6 символов'),
})

export default function SettingsPage() {
  const user = useAuthStore((s) => s.user)

  const profileForm = useForm({ resolver: zodResolver(profileSchema), defaultValues: { name: user?.name ?? '', email: user?.email ?? '' } })
  const passwordForm = useForm({ resolver: zodResolver(passwordSchema) })

  const profileMutation = useMutation({
    mutationFn: (data: { name: string; email: string }) => api.put('/auth/me', data),
    onSuccess: () => toast.success('Профиль обновлён'),
    onError: () => toast.error('Ошибка обновления'),
  })

  const passwordMutation = useMutation({
    mutationFn: (data: { currentPassword: string; newPassword: string }) => api.put('/auth/password', data),
    onSuccess: () => { toast.success('Пароль изменён'); passwordForm.reset() },
    onError: () => toast.error('Неверный текущий пароль'),
  })

  return (
    <div className="p-8 max-w-xl space-y-8">
      <h1 className="text-2xl font-bold">Настройки аккаунта</h1>

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Личные данные</h2>
        <form onSubmit={profileForm.handleSubmit((d) => profileMutation.mutate(d))} className="space-y-4">
          <div className="space-y-1">
            <Label>Имя</Label>
            <Input {...profileForm.register('name')} />
            {profileForm.formState.errors.name && <p className="text-sm text-red-500">{profileForm.formState.errors.name.message}</p>}
          </div>
          <div className="space-y-1">
            <Label>Email</Label>
            <Input type="email" {...profileForm.register('email')} />
            {profileForm.formState.errors.email && <p className="text-sm text-red-500">{profileForm.formState.errors.email.message}</p>}
          </div>
          <Button type="submit" disabled={profileMutation.isPending}>
            {profileMutation.isPending ? 'Сохранение...' : 'Сохранить'}
          </Button>
        </form>
      </section>

      <Separator />

      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Изменить пароль</h2>
        <form onSubmit={passwordForm.handleSubmit((d) => passwordMutation.mutate(d))} className="space-y-4">
          <div className="space-y-1">
            <Label>Текущий пароль</Label>
            <Input type="password" {...passwordForm.register('currentPassword')} />
          </div>
          <div className="space-y-1">
            <Label>Новый пароль</Label>
            <Input type="password" {...passwordForm.register('newPassword')} />
            {passwordForm.formState.errors.newPassword && <p className="text-sm text-red-500">{passwordForm.formState.errors.newPassword.message}</p>}
          </div>
          <Button type="submit" disabled={passwordMutation.isPending}>
            {passwordMutation.isPending ? 'Изменение...' : 'Изменить пароль'}
          </Button>
        </form>
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
git commit -m "feat(frontend): add posts, tasks, settings pages (Phase 5c complete)"
```
