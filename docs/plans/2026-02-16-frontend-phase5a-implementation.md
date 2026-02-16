# Frontend Phase 5a Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Scaffold the Next.js 14 frontend service with auth (login/register), app shell (sidebar), chat page with SSE streaming + expandable tool cards, and integrations page.

**Architecture:** Next.js 14 App Router; `(public)` route group for unauthenticated pages, `(app)` route group for protected pages with sidebar layout. TanStack Query for REST API calls, Zustand for auth state, Axios with automatic 401→refresh interceptor. SSE streaming via native `fetch` + `ReadableStream`.

**Tech Stack:** Next.js 14, React 18, TypeScript, Tailwind CSS, shadcn/ui, TanStack Query v5, Zustand, Axios, react-hook-form, zod, Vitest, React Testing Library.

---

### Task 1: Project scaffold

**Files:**
- Create: `services/frontend/` (entire directory)

**Context:**
All frontend code lives in `services/frontend/`. This task sets up the Next.js project, installs all dependencies, configures Tailwind, initialises shadcn/ui, and sets up the proxy rewrites.

**Step 1: Create the Next.js project**

```bash
cd /Users/f1xgun/onevoice/services
npx create-next-app@14 frontend \
  --typescript \
  --tailwind \
  --eslint \
  --app \
  --src-dir=false \
  --import-alias="@/*" \
  --no-git
```

**Step 2: Install additional dependencies**

```bash
cd /Users/f1xgun/onevoice/services/frontend

# Data fetching + state
pnpm add @tanstack/react-query@5 zustand axios

# Forms + validation
pnpm add react-hook-form @hookform/resolvers zod

# UI utilities
pnpm add class-variance-authority clsx tailwind-merge lucide-react

# shadcn/ui peer deps
pnpm add @radix-ui/react-dialog @radix-ui/react-dropdown-menu \
  @radix-ui/react-label @radix-ui/react-select @radix-ui/react-slot \
  @radix-ui/react-toast @radix-ui/react-separator @radix-ui/react-avatar \
  @radix-ui/react-badge

# Sonner for toasts (better than shadcn built-in)
pnpm add sonner

# Dev / test
pnpm add -D vitest @vitejs/plugin-react jsdom \
  @testing-library/react @testing-library/user-event \
  @testing-library/jest-dom @types/testing-library__jest-dom
```

**Step 3: Initialise shadcn/ui**

```bash
cd /Users/f1xgun/onevoice/services/frontend
pnpm dlx shadcn-ui@latest init
```

When prompted:
- Style: **Default**
- Base color: **Slate**
- CSS variables: **Yes**

Then add the components we'll use:
```bash
pnpm dlx shadcn-ui@latest add button input label card badge \
  dialog form select separator avatar toast
```

**Step 4: Write `next.config.js`**

Replace `services/frontend/next.config.js` (or `.ts`) with:

```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${process.env.API_URL || 'http://localhost:8080'}/api/v1/:path*`,
      },
      {
        source: '/chat/:path*',
        destination: `${process.env.ORCHESTRATOR_URL || 'http://localhost:8090'}/chat/:path*`,
      },
    ]
  },
}

module.exports = nextConfig
```

**Step 5: Configure Vitest**

Create `services/frontend/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
})
```

Create `services/frontend/vitest.setup.ts`:

```ts
import '@testing-library/jest-dom'
```

Add test script to `package.json`:

```json
"scripts": {
  "test": "vitest run",
  "test:watch": "vitest"
}
```

**Step 6: Verify build**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm build
```
Expected: successful build, no TypeScript errors.

**Step 7: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): scaffold Next.js 14 with Tailwind, shadcn/ui, TanStack Query setup"
```

---

### Task 2: Core libs — Axios client + Zustand auth store + QueryClient

**Files:**
- Create: `services/frontend/lib/api.ts`
- Create: `services/frontend/lib/auth.ts`
- Create: `services/frontend/lib/query.ts`
- Create: `services/frontend/lib/__tests__/auth.test.ts`

**Context:**
`lib/auth.ts` is a Zustand store holding `user`, `accessToken`, `isAuthenticated`. `lib/api.ts` is an Axios instance that reads the access token from the auth store and handles 401 by calling refresh. `lib/query.ts` exports a singleton `QueryClient`.

**Step 1: Write failing tests**

Create `services/frontend/lib/__tests__/auth.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { useAuthStore } from '../auth'

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, accessToken: null })
    localStorage.clear()
  })

  it('sets user and token on login', () => {
    const user = { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' }
    useAuthStore.getState().setAuth(user, 'access-token', 'refresh-token')

    expect(useAuthStore.getState().user).toEqual(user)
    expect(useAuthStore.getState().accessToken).toBe('access-token')
    expect(useAuthStore.getState().isAuthenticated).toBe(true)
    expect(localStorage.getItem('refreshToken')).toBe('refresh-token')
  })

  it('clears state on logout', () => {
    useAuthStore.getState().setAuth(
      { id: '1', email: 'test@test.com', name: 'Test', role: 'owner' },
      'access-token',
      'refresh-token'
    )
    useAuthStore.getState().logout()

    expect(useAuthStore.getState().user).toBeNull()
    expect(useAuthStore.getState().accessToken).toBeNull()
    expect(useAuthStore.getState().isAuthenticated).toBe(false)
    expect(localStorage.getItem('refreshToken')).toBeNull()
  })
})
```

**Step 2: Run to verify RED**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: FAIL — `useAuthStore` not found

**Step 3: Implement `lib/auth.ts`**

```ts
import { create } from 'zustand'

export type UserRole = 'owner' | 'admin' | 'member'

export interface User {
  id: string
  email: string
  name: string
  role: UserRole
}

interface AuthState {
  user: User | null
  accessToken: string | null
  isAuthenticated: boolean
  setAuth: (user: User, accessToken: string, refreshToken: string) => void
  setAccessToken: (token: string) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  accessToken: null,
  isAuthenticated: false,

  setAuth: (user, accessToken, refreshToken) => {
    localStorage.setItem('refreshToken', refreshToken)
    set({ user, accessToken, isAuthenticated: true })
  },

  setAccessToken: (token) => {
    set({ accessToken: token })
  },

  logout: () => {
    localStorage.removeItem('refreshToken')
    set({ user: null, accessToken: null, isAuthenticated: false })
  },
}))
```

**Step 4: Implement `lib/api.ts`**

```ts
import axios from 'axios'
import { useAuthStore } from './auth'

export const api = axios.create({
  baseURL: '/api/v1',
  headers: { 'Content-Type': 'application/json' },
})

// Attach access token to every request
api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// On 401: try refresh once, then logout
let refreshing = false
let queue: Array<{ resolve: (v: string) => void; reject: (e: unknown) => void }> = []

api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const original = error.config

    if (error.response?.status !== 401 || original._retry) {
      return Promise.reject(error)
    }

    if (refreshing) {
      return new Promise((resolve, reject) => {
        queue.push({ resolve, reject })
      }).then((token) => {
        original.headers.Authorization = `Bearer ${token}`
        return api(original)
      })
    }

    original._retry = true
    refreshing = true

    try {
      const refreshToken = localStorage.getItem('refreshToken')
      if (!refreshToken) throw new Error('no refresh token')

      const { data } = await axios.post('/api/v1/auth/refresh', { refreshToken })
      const { accessToken, refreshToken: newRefresh } = data

      useAuthStore.getState().setAccessToken(accessToken)
      localStorage.setItem('refreshToken', newRefresh)

      queue.forEach(({ resolve }) => resolve(accessToken))
      queue = []

      original.headers.Authorization = `Bearer ${accessToken}`
      return api(original)
    } catch {
      queue.forEach(({ reject }) => reject(error))
      queue = []
      useAuthStore.getState().logout()
      window.location.href = '/login'
      return Promise.reject(error)
    } finally {
      refreshing = false
    }
  }
)
```

**Step 5: Implement `lib/query.ts`**

```ts
import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      retry: 1,
    },
  },
})
```

**Step 6: Run tests to verify GREEN**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: all tests PASS

**Step 7: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/lib/
git commit -m "feat(frontend): add Axios client with refresh interceptor and Zustand auth store"
```

---

### Task 3: Root layout + Auth pages (Login, Register)

**Files:**
- Create: `services/frontend/app/layout.tsx`
- Create: `services/frontend/app/(public)/login/page.tsx`
- Create: `services/frontend/app/(public)/register/page.tsx`
- Create: `services/frontend/lib/schemas.ts`
- Create: `services/frontend/lib/__tests__/schemas.test.ts`

**Context:**
Root layout wraps the app with `QueryClientProvider` and `Toaster`. Login and Register use react-hook-form + zod. On success they call `useAuthStore.setAuth()` and redirect to `/chat`.

**Step 1: Write failing validation tests**

Create `services/frontend/lib/__tests__/schemas.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { loginSchema, registerSchema } from '../schemas'

describe('loginSchema', () => {
  it('rejects empty email', () => {
    const result = loginSchema.safeParse({ email: '', password: 'pass123' })
    expect(result.success).toBe(false)
  })

  it('rejects short password', () => {
    const result = loginSchema.safeParse({ email: 'a@b.com', password: '12' })
    expect(result.success).toBe(false)
  })

  it('accepts valid credentials', () => {
    const result = loginSchema.safeParse({ email: 'a@b.com', password: 'password123' })
    expect(result.success).toBe(true)
  })
})

describe('registerSchema', () => {
  it('rejects mismatched passwords', () => {
    const result = registerSchema.safeParse({
      name: 'Test', email: 'a@b.com',
      password: 'password123', confirmPassword: 'different',
    })
    expect(result.success).toBe(false)
  })
})
```

**Step 2: Run to verify RED**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: FAIL — `loginSchema` not found

**Step 3: Create `lib/schemas.ts`**

```ts
import { z } from 'zod'

export const loginSchema = z.object({
  email: z.string().email('Некорректный email'),
  password: z.string().min(6, 'Минимум 6 символов'),
})

export const registerSchema = z.object({
  name: z.string().min(2, 'Минимум 2 символа'),
  email: z.string().email('Некорректный email'),
  password: z.string().min(6, 'Минимум 6 символов'),
  confirmPassword: z.string(),
}).refine((d) => d.password === d.confirmPassword, {
  message: 'Пароли не совпадают',
  path: ['confirmPassword'],
})

export type LoginInput = z.infer<typeof loginSchema>
export type RegisterInput = z.infer<typeof registerSchema>
```

**Step 4: Create root layout `app/layout.tsx`**

```tsx
import type { Metadata } from 'next'
import { Inter } from 'next/font/google'
import './globals.css'
import { Providers } from '@/components/providers'

const inter = Inter({ subsets: ['latin', 'cyrillic'] })

export const metadata: Metadata = {
  title: 'OneVoice — управление цифровым присутствием',
  description: 'Мультиагентная система для автоматизации SMB',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ru">
      <body className={inter.className}>
        <Providers>{children}</Providers>
      </body>
    </html>
  )
}
```

Create `services/frontend/components/providers.tsx`:

```tsx
'use client'

import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from '@/lib/query'
import { Toaster } from 'sonner'

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <Toaster richColors position="top-right" />
    </QueryClientProvider>
  )
}
```

**Step 5: Create `app/(public)/login/page.tsx`**

```tsx
'use client'

import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useRouter } from 'next/navigation'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuthStore } from '@/lib/auth'
import { loginSchema, type LoginInput } from '@/lib/schemas'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export default function LoginPage() {
  const router = useRouter()
  const setAuth = useAuthStore((s) => s.setAuth)

  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<LoginInput>({
    resolver: zodResolver(loginSchema),
  })

  const onSubmit = async (data: LoginInput) => {
    try {
      const res = await api.post('/auth/login', data)
      setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
      router.push('/chat')
    } catch {
      toast.error('Неверный email или пароль')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-2xl text-center">Войти в OneVoice</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-1">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" {...register('email')} />
              {errors.email && <p className="text-sm text-red-500">{errors.email.message}</p>}
            </div>
            <div className="space-y-1">
              <Label htmlFor="password">Пароль</Label>
              <Input id="password" type="password" {...register('password')} />
              {errors.password && <p className="text-sm text-red-500">{errors.password.message}</p>}
            </div>
            <Button type="submit" className="w-full" disabled={isSubmitting}>
              {isSubmitting ? 'Вход...' : 'Войти'}
            </Button>
            <p className="text-center text-sm text-gray-500">
              Нет аккаунта?{' '}
              <a href="/register" className="text-blue-600 hover:underline">Зарегистрироваться</a>
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
```

**Step 6: Create `app/(public)/register/page.tsx`**

```tsx
'use client'

import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useRouter } from 'next/navigation'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuthStore } from '@/lib/auth'
import { registerSchema, type RegisterInput } from '@/lib/schemas'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export default function RegisterPage() {
  const router = useRouter()
  const setAuth = useAuthStore((s) => s.setAuth)

  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<RegisterInput>({
    resolver: zodResolver(registerSchema),
  })

  const onSubmit = async (data: RegisterInput) => {
    try {
      const res = await api.post('/auth/register', {
        name: data.name,
        email: data.email,
        password: data.password,
      })
      setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
      router.push('/chat')
    } catch {
      toast.error('Ошибка регистрации. Проверьте данные.')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-2xl text-center">Создать аккаунт</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-1">
              <Label htmlFor="name">Имя</Label>
              <Input id="name" {...register('name')} />
              {errors.name && <p className="text-sm text-red-500">{errors.name.message}</p>}
            </div>
            <div className="space-y-1">
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" {...register('email')} />
              {errors.email && <p className="text-sm text-red-500">{errors.email.message}</p>}
            </div>
            <div className="space-y-1">
              <Label htmlFor="password">Пароль</Label>
              <Input id="password" type="password" {...register('password')} />
              {errors.password && <p className="text-sm text-red-500">{errors.password.message}</p>}
            </div>
            <div className="space-y-1">
              <Label htmlFor="confirmPassword">Повторите пароль</Label>
              <Input id="confirmPassword" type="password" {...register('confirmPassword')} />
              {errors.confirmPassword && <p className="text-sm text-red-500">{errors.confirmPassword.message}</p>}
            </div>
            <Button type="submit" className="w-full" disabled={isSubmitting}>
              {isSubmitting ? 'Регистрация...' : 'Зарегистрироваться'}
            </Button>
            <p className="text-center text-sm text-gray-500">
              Уже есть аккаунт?{' '}
              <a href="/login" className="text-blue-600 hover:underline">Войти</a>
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
```

**Step 7: Run tests and build**

```bash
cd /Users/f1xgun/onevoice/services/frontend
pnpm test       # schemas tests should pass
pnpm build      # must compile cleanly
```
Expected: tests PASS, build succeeds

**Step 8: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add root layout, auth store providers, login and register pages"
```

---

### Task 4: App shell — protected layout with sidebar

**Files:**
- Create: `services/frontend/app/(app)/layout.tsx`
- Create: `services/frontend/components/sidebar.tsx`

**Context:**
The `(app)` layout renders the sidebar and protects routes. If the user is not authenticated (no Zustand token AND no localStorage refresh token), redirect to `/login`. On first render, attempt silent token refresh using the stored refresh token.

**Step 1: Create `components/sidebar.tsx`**

```tsx
'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { MessageCircle, Plug, Building2, Star, FileText, ListTodo, Settings, LogOut } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/lib/auth'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

const navItems = [
  { href: '/chat', label: 'Чат', icon: MessageCircle },
  { href: '/integrations', label: 'Интеграции', icon: Plug },
  { href: '/business', label: 'Бизнес', icon: Building2 },
  { href: '/reviews', label: 'Отзывы', icon: Star },
  { href: '/posts', label: 'Посты', icon: FileText },
  { href: '/tasks', label: 'Задачи', icon: ListTodo },
  { href: '/settings', label: 'Настройки', icon: Settings },
]

const platformColors: Record<string, string> = {
  telegram: '#2AABEE',
  vk: '#4680C2',
  yandex_business: '#FC3F1D',
}

const platformLabels: Record<string, string> = {
  telegram: 'Telegram',
  vk: 'ВКонтакте',
  yandex_business: 'Яндекс.Бизнес',
}

export function Sidebar() {
  const pathname = usePathname()
  const logout = useAuthStore((s) => s.logout)
  const user = useAuthStore((s) => s.user)

  const { data: integrations } = useQuery({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => r.data.integrations ?? []),
  })

  return (
    <aside className="w-60 h-screen bg-gray-900 text-white flex flex-col shrink-0">
      {/* Logo */}
      <div className="p-4 border-b border-gray-700">
        <h1 className="text-xl font-bold text-white">OneVoice</h1>
        <p className="text-xs text-gray-400 truncate">{user?.email}</p>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-2 space-y-1">
        {navItems.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={cn(
              'flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors',
              pathname.startsWith(href)
                ? 'bg-gray-700 text-white'
                : 'text-gray-300 hover:bg-gray-800 hover:text-white'
            )}
          >
            <Icon size={18} />
            {label}
          </Link>
        ))}
      </nav>

      {/* Platform status */}
      <div className="p-4 border-t border-gray-700 space-y-2">
        <p className="text-xs text-gray-500 uppercase tracking-wide">Платформы</p>
        {['telegram', 'vk', 'yandex_business'].map((platform) => {
          const integration = integrations?.find((i: { platform: string }) => i.platform === platform)
          const connected = integration?.status === 'active'
          return (
            <div key={platform} className="flex items-center gap-2 text-xs text-gray-300">
              <span
                className="w-2 h-2 rounded-full"
                style={{ backgroundColor: connected ? '#22c55e' : '#6b7280' }}
              />
              {platformLabels[platform]}
            </div>
          )
        })}
      </div>

      {/* Logout */}
      <button
        onClick={logout}
        className="flex items-center gap-3 p-4 text-gray-400 hover:text-white hover:bg-gray-800 text-sm border-t border-gray-700"
      >
        <LogOut size={18} />
        Выйти
      </button>
    </aside>
  )
}
```

**Step 2: Create `app/(app)/layout.tsx`**

```tsx
'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { useAuthStore } from '@/lib/auth'
import { api } from '@/lib/api'
import { Sidebar } from '@/components/sidebar'

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter()
  const { isAuthenticated, setAuth, accessToken } = useAuthStore()

  useEffect(() => {
    const refreshToken = localStorage.getItem('refreshToken')

    if (!refreshToken) {
      router.replace('/login')
      return
    }

    if (!accessToken) {
      // Attempt silent refresh
      api.post('/auth/refresh', { refreshToken })
        .then((res) => {
          setAuth(res.data.user, res.data.accessToken, res.data.refreshToken)
        })
        .catch(() => {
          router.replace('/login')
        })
    }
  }, [])

  // While restoring session, show nothing (avoids flash)
  if (!isAuthenticated && !localStorage.getItem('refreshToken')) {
    return null
  }

  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-y-auto bg-gray-50">
        {children}
      </main>
    </div>
  )
}
```

**Step 3: Add placeholder pages so navigation links don't 404**

Create `services/frontend/app/(app)/integrations/page.tsx`, `reviews/page.tsx`, `posts/page.tsx`, `tasks/page.tsx`, `settings/page.tsx`, `business/page.tsx` — each is a temporary stub:

```tsx
// Example for each page — same pattern
export default function IntegrationsPage() {
  return <div className="p-8"><h1 className="text-2xl font-bold">Интеграции</h1></div>
}
```

(Replace `IntegrationsPage` and heading text for each route.)

**Step 4: Verify dev server**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm dev
```
Open `http://localhost:3000/login` — should see login form.
Navigate to `http://localhost:3000/chat` — should redirect to `/login`.

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add app shell layout with sidebar, auth guard, and page stubs"
```

---

### Task 5: SSE streaming hook — `useChat`

**Files:**
- Create: `services/frontend/hooks/useChat.ts`
- Create: `services/frontend/hooks/__tests__/useChat.test.ts`

**Context:**
The orchestrator at `POST /chat/{conversationId}` returns an SSE stream. Each event is a JSON line:
```
data: {"type":"text","content":"..."}\n\n
data: {"type":"tool_call","tool_name":"vk__publish_post","tool_args":{...}}\n\n
data: {"type":"tool_result","tool_name":"vk__publish_post","result":{...}}\n\n
data: {"type":"done"}\n\n
```

The hook accumulates these into a `Message` array and exposes `sendMessage(text)`.

**Step 1: Define types**

Create `services/frontend/types/chat.ts`:

```ts
export type ToolCallStatus = 'pending' | 'done' | 'error'

export interface ToolCall {
  name: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  error?: string
  status: ToolCallStatus
}

export interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  toolCalls?: ToolCall[]
  status?: 'streaming' | 'done'
}
```

**Step 2: Write failing tests for the SSE event parser**

Create `services/frontend/hooks/__tests__/useChat.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { parseSSELine, applySSEEvent } from '../useChat'
import type { Message } from '@/types/chat'

describe('parseSSELine', () => {
  it('returns null for non-data lines', () => {
    expect(parseSSELine('')).toBeNull()
    expect(parseSSELine(': keep-alive')).toBeNull()
  })

  it('parses data line to object', () => {
    const result = parseSSELine('data: {"type":"text","content":"hello"}')
    expect(result).toEqual({ type: 'text', content: 'hello' })
  })
})

describe('applySSEEvent', () => {
  const baseMessage: Message = {
    id: '1',
    role: 'assistant',
    content: '',
    toolCalls: [],
    status: 'streaming',
  }

  it('appends text to content', () => {
    const result = applySSEEvent(baseMessage, { type: 'text', content: ' world' })
    expect(result.content).toBe(' world')
  })

  it('adds tool_call entry as pending', () => {
    const result = applySSEEvent(baseMessage, {
      type: 'tool_call',
      tool_name: 'vk__publish_post',
      tool_args: { text: 'hello' },
    })
    expect(result.toolCalls).toHaveLength(1)
    expect(result.toolCalls![0].status).toBe('pending')
    expect(result.toolCalls![0].name).toBe('vk__publish_post')
  })

  it('updates tool_call to done on tool_result', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [{ name: 'vk__publish_post', args: {}, status: 'pending' }],
    }
    const result = applySSEEvent(msg, {
      type: 'tool_result',
      tool_name: 'vk__publish_post',
      result: { post_id: '123' },
    })
    expect(result.toolCalls![0].status).toBe('done')
    expect(result.toolCalls![0].result).toEqual({ post_id: '123' })
  })

  it('marks done on done event', () => {
    const result = applySSEEvent(baseMessage, { type: 'done' })
    expect(result.status).toBe('done')
  })
})
```

**Step 3: Run to verify RED**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: FAIL — `parseSSELine` not found

**Step 4: Implement `hooks/useChat.ts`**

```ts
'use client'

import { useState, useCallback, useRef } from 'react'
import { useAuthStore } from '@/lib/auth'
import type { Message, ToolCall } from '@/types/chat'

// Exported for unit testing
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null
  try {
    return JSON.parse(line.slice(6))
  } catch {
    return null
  }
}

export function applySSEEvent(
  msg: Message,
  event: Record<string, unknown>
): Message {
  const type = event.type as string

  if (type === 'text') {
    return { ...msg, content: msg.content + (event.content as string) }
  }

  if (type === 'tool_call') {
    const toolCall: ToolCall = {
      name: event.tool_name as string,
      args: (event.tool_args as Record<string, unknown>) ?? {},
      status: 'pending',
    }
    return { ...msg, toolCalls: [...(msg.toolCalls ?? []), toolCall] }
  }

  if (type === 'tool_result') {
    const toolName = event.tool_name as string
    const updated = (msg.toolCalls ?? []).map((tc) =>
      tc.name === toolName
        ? {
            ...tc,
            result: event.result as Record<string, unknown>,
            error: event.error as string | undefined,
            status: (event.error ? 'error' : 'done') as ToolCall['status'],
          }
        : tc
    )
    return { ...msg, toolCalls: updated }
  }

  if (type === 'done') {
    return { ...msg, status: 'done' }
  }

  return msg
}

export function useChat(conversationId: string) {
  const [messages, setMessages] = useState<Message[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const accessToken = useAuthStore((s) => s.accessToken)
  const abortRef = useRef<AbortController | null>(null)

  const sendMessage = useCallback(
    async (text: string) => {
      if (isStreaming) return

      const userMessage: Message = {
        id: crypto.randomUUID(),
        role: 'user',
        content: text,
        status: 'done',
      }

      const assistantMessage: Message = {
        id: crypto.randomUUID(),
        role: 'assistant',
        content: '',
        toolCalls: [],
        status: 'streaming',
      }

      setMessages((prev) => [...prev, userMessage, assistantMessage])
      setIsStreaming(true)

      const controller = new AbortController()
      abortRef.current = controller

      try {
        const response = await fetch(`/chat/${conversationId}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${accessToken}`,
          },
          body: JSON.stringify({ message: text }),
          signal: controller.signal,
        })

        const reader = response.body!.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? ''

          for (const line of lines) {
            const event = parseSSELine(line.trim())
            if (!event) continue
            setMessages((prev) => {
              const last = prev[prev.length - 1]
              if (last.role !== 'assistant') return prev
              return [...prev.slice(0, -1), applySSEEvent(last, event)]
            })
          }
        }
      } catch (error: unknown) {
        if ((error as Error).name === 'AbortError') return
        setMessages((prev) => {
          const last = prev[prev.length - 1]
          if (last.role !== 'assistant') return prev
          return [...prev.slice(0, -1), { ...last, content: 'Ошибка соединения', status: 'done' }]
        })
      } finally {
        setIsStreaming(false)
      }
    },
    [conversationId, accessToken, isStreaming]
  )

  const stop = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  return { messages, isStreaming, sendMessage, stop }
}
```

**Step 5: Run tests to verify GREEN**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm test
```
Expected: all tests PASS

**Step 6: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/hooks/ services/frontend/types/
git commit -m "feat(frontend): add useChat SSE streaming hook with parseSSELine and applySSEEvent"
```

---

### Task 6: Chat page — UI components + page

**Files:**
- Create: `services/frontend/components/chat/ToolCard.tsx`
- Create: `services/frontend/components/chat/ToolCallsBlock.tsx`
- Create: `services/frontend/components/chat/MessageBubble.tsx`
- Create: `services/frontend/components/chat/ChatWindow.tsx`
- Create: `services/frontend/app/(app)/chat/page.tsx`
- Create: `services/frontend/app/(app)/chat/[id]/page.tsx`

**Context:**
`ToolCard` renders a single tool action. `ToolCallsBlock` renders the expandable toggle + list of `ToolCard`s inside an assistant message. `MessageBubble` wraps a message. `ChatWindow` holds the message list + input.

**Step 1: Create `components/chat/ToolCard.tsx`**

```tsx
import { Badge } from '@/components/ui/badge'
import type { ToolCall } from '@/types/chat'

const platformColors: Record<string, string> = {
  vk: '#4680C2',
  telegram: '#2AABEE',
  yandex_business: '#FC3F1D',
}

const platformLabels: Record<string, string> = {
  vk: 'VK',
  telegram: 'TG',
  yandex_business: 'YB',
}

function getPlatform(toolName: string): string {
  const prefix = toolName.split('__')[0]
  return prefix ?? toolName
}

export function ToolCard({ tool }: { tool: ToolCall }) {
  const platform = getPlatform(tool.name)
  const color = platformColors[platform] ?? '#6b7280'
  const label = platformLabels[platform] ?? platform.toUpperCase()

  return (
    <div className="border rounded-md p-3 text-sm space-y-1" style={{ borderLeftColor: color, borderLeftWidth: 3 }}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span
            className="px-1.5 py-0.5 rounded text-white text-xs font-bold"
            style={{ backgroundColor: color }}
          >
            {label}
          </span>
          <span className="font-mono text-xs text-gray-600">{tool.name}</span>
        </div>
        {tool.status === 'pending' && (
          <span className="w-4 h-4 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin" />
        )}
        {tool.status === 'done' && <span className="text-green-500">✅</span>}
        {tool.status === 'error' && <span className="text-red-500">❌</span>}
      </div>
      {tool.result && (
        <p className="text-gray-500 text-xs truncate">
          {JSON.stringify(tool.result).slice(0, 80)}
        </p>
      )}
      {tool.error && <p className="text-red-500 text-xs">{tool.error}</p>}
    </div>
  )
}
```

**Step 2: Create `components/chat/ToolCallsBlock.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { ToolCard } from './ToolCard'
import type { ToolCall } from '@/types/chat'

const platformColors: Record<string, string> = {
  vk: '#4680C2',
  telegram: '#2AABEE',
  yandex_business: '#FC3F1D',
}

function PlatformBadge({ name }: { name: string }) {
  const platform = name.split('__')[0]
  const color = platformColors[platform] ?? '#6b7280'
  const labels: Record<string, string> = { vk: 'VK', telegram: 'TG', yandex_business: 'YB' }
  return (
    <span
      className="px-1.5 py-0.5 rounded text-white text-xs font-bold"
      style={{ backgroundColor: color }}
    >
      {labels[platform] ?? platform.toUpperCase()}
    </span>
  )
}

export function ToolCallsBlock({ toolCalls }: { toolCalls: ToolCall[] }) {
  const [expanded, setExpanded] = useState(false)
  if (toolCalls.length === 0) return null

  const doneCount = toolCalls.filter((t) => t.status === 'done').length
  const platforms = [...new Set(toolCalls.map((t) => t.name.split('__')[0]))]

  return (
    <div className="mt-2 border border-gray-200 rounded-md overflow-hidden">
      <button
        onClick={() => setExpanded((e) => !e)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-gray-50 hover:bg-gray-100 text-sm text-left"
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span className="text-gray-600">
          {expanded ? 'Скрыть' : 'Показать'} действия ({toolCalls.length})
        </span>
        <span className="text-green-600 text-xs ml-1">✓ {doneCount}/{toolCalls.length}</span>
        <div className="flex gap-1 ml-auto">
          {platforms.map((p) => <PlatformBadge key={p} name={p + '__x'} />)}
        </div>
      </button>

      {expanded && (
        <div className="p-2 space-y-2 bg-white">
          {toolCalls.map((tool, i) => (
            <ToolCard key={i} tool={tool} />
          ))}
        </div>
      )}
    </div>
  )
}
```

**Step 3: Create `components/chat/MessageBubble.tsx`**

```tsx
import { ToolCallsBlock } from './ToolCallsBlock'
import type { Message } from '@/types/chat'

export function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === 'user'

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}>
      <div className={`max-w-[75%] ${isUser ? 'order-2' : 'order-1'}`}>
        <div
          className={`rounded-2xl px-4 py-3 text-sm ${
            isUser
              ? 'bg-blue-600 text-white rounded-br-sm'
              : 'bg-white border border-gray-200 text-gray-800 rounded-bl-sm'
          }`}
        >
          {message.status === 'streaming' && !message.content ? (
            <span className="flex gap-1">
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce [animation-delay:0ms]" />
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce [animation-delay:150ms]" />
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce [animation-delay:300ms]" />
            </span>
          ) : (
            <p className="whitespace-pre-wrap">{message.content}</p>
          )}
          {!isUser && message.toolCalls && message.toolCalls.length > 0 && (
            <ToolCallsBlock toolCalls={message.toolCalls} />
          )}
        </div>
      </div>
    </div>
  )
}
```

**Step 4: Create `components/chat/ChatWindow.tsx`**

```tsx
'use client'

import { useRef, useEffect, useState } from 'react'
import { Send } from 'lucide-react'
import { MessageBubble } from './MessageBubble'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useChat } from '@/hooks/useChat'

const QUICK_ACTIONS = ['Проверить отзывы', 'Обновить часы работы', 'Опубликовать пост']

export function ChatWindow({ conversationId }: { conversationId: string }) {
  const { messages, isStreaming, sendMessage } = useChat(conversationId)
  const [input, setInput] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSend = async () => {
    const text = input.trim()
    if (!text || isStreaming) return
    setInput('')
    await sendMessage(text)
  }

  return (
    <div className="flex flex-col h-full">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-6">
        {messages.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center gap-4">
            <p className="text-gray-400 text-lg">Чем могу помочь?</p>
            <div className="flex flex-wrap gap-2 justify-center">
              {QUICK_ACTIONS.map((action) => (
                <button
                  key={action}
                  onClick={() => sendMessage(action)}
                  className="px-4 py-2 rounded-full border border-gray-200 text-sm text-gray-600 hover:bg-gray-50"
                >
                  {action}
                </button>
              ))}
            </div>
          </div>
        ) : (
          messages.map((msg) => <MessageBubble key={msg.id} message={msg} />)
        )}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <div className="border-t bg-white p-4 flex gap-2">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && handleSend()}
          placeholder="Напишите сообщение..."
          disabled={isStreaming}
          className="flex-1"
        />
        <Button onClick={handleSend} disabled={isStreaming || !input.trim()}>
          <Send size={16} />
        </Button>
      </div>
    </div>
  )
}
```

**Step 5: Create chat pages**

`services/frontend/app/(app)/chat/page.tsx` (redirects to a new conversation):

```tsx
'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'

export default function ChatIndexPage() {
  const router = useRouter()

  const { mutate: createConversation } = useMutation({
    mutationFn: () => api.post('/conversations').then((r) => r.data.conversation),
    onSuccess: (conv) => router.replace(`/chat/${conv.id}`),
  })

  useEffect(() => { createConversation() }, [])

  return (
    <div className="h-full flex items-center justify-center text-gray-400">
      Создание диалога...
    </div>
  )
}
```

`services/frontend/app/(app)/chat/[id]/page.tsx`:

```tsx
import { ChatWindow } from '@/components/chat/ChatWindow'

export default function ConversationPage({ params }: { params: { id: string } }) {
  return <ChatWindow conversationId={params.id} />
}
```

**Step 6: Verify with dev server**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm dev
```
Login → should redirect to `/chat` → chat window loads with zero-state quick actions.

```bash
pnpm test
```
Expected: all tests PASS

**Step 7: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add chat page with SSE streaming and expandable tool cards"
```

---

### Task 7: Integrations page

**Files:**
- Create: `services/frontend/components/integrations/PlatformCard.tsx`
- Create: `services/frontend/components/integrations/ConnectDialog.tsx`
- Modify: `services/frontend/app/(app)/integrations/page.tsx`

**Context:**
`GET /api/v1/integrations` returns `{integrations: [{platform, status, last_sync_at}]}`. The page renders MVP platforms (telegram, vk, yandex_business) as cards with status and connect/disconnect actions.

**Step 1: Create `components/integrations/PlatformCard.tsx`**

```tsx
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

interface Props {
  platform: string
  label: string
  description: string
  color: string
  status: 'active' | 'inactive' | 'error' | null
  lastSyncAt?: string
  onConnect: () => void
  onDisconnect: () => void
  disabled?: boolean
}

const statusLabels = {
  active: 'Подключено',
  inactive: 'Отключено',
  error: 'Ошибка',
}

const statusVariants: Record<string, 'default' | 'secondary' | 'destructive'> = {
  active: 'default',
  inactive: 'secondary',
  error: 'destructive',
}

export function PlatformCard({
  label, description, color, status, lastSyncAt,
  onConnect, onDisconnect, disabled,
}: Props) {
  const connected = status === 'active'

  return (
    <Card className={disabled ? 'opacity-40 pointer-events-none' : ''}>
      <CardContent className="p-5 space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg flex items-center justify-center text-white font-bold text-sm"
              style={{ backgroundColor: color }}>
              {label.slice(0, 2).toUpperCase()}
            </div>
            <div>
              <p className="font-medium">{label}</p>
              <p className="text-xs text-gray-500">{description}</p>
            </div>
          </div>
          <Badge variant={statusVariants[status ?? 'inactive']}>
            {statusLabels[status ?? 'inactive']}
          </Badge>
        </div>

        {lastSyncAt && (
          <p className="text-xs text-gray-400">
            Синхронизировано: {new Date(lastSyncAt).toLocaleString('ru')}
          </p>
        )}

        <div className="flex gap-2">
          {connected ? (
            <Button variant="outline" size="sm" onClick={onDisconnect} className="text-red-600 border-red-200">
              Отключить
            </Button>
          ) : (
            <Button size="sm" onClick={onConnect}>
              Подключить
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
```

**Step 2: Create `components/integrations/ConnectDialog.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

interface Props {
  platform: string
  open: boolean
  onClose: () => void
  onConnect: (credentials: Record<string, string>) => Promise<void>
}

const configs: Record<string, { label: string; fields: { key: string; label: string; placeholder: string }[] }> = {
  telegram: {
    label: 'Подключить Telegram',
    fields: [{ key: 'bot_token', label: 'Bot Token', placeholder: 'Получите у @BotFather' }],
  },
  vk: {
    label: 'Подключить ВКонтакте',
    fields: [{ key: 'access_token', label: 'Access Token', placeholder: 'Токен доступа VK' }],
  },
  yandex_business: {
    label: 'Подключить Яндекс.Бизнес',
    fields: [
      { key: 'cookies', label: 'Cookies JSON', placeholder: 'Скопируйте cookies из браузера' },
    ],
  },
}

export function ConnectDialog({ platform, open, onClose, onConnect }: Props) {
  const config = configs[platform]
  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)

  if (!config) return null

  const handleSubmit = async () => {
    setLoading(true)
    try {
      await onConnect(values)
      onClose()
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{config.label}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          {config.fields.map((field) => (
            <div key={field.key} className="space-y-1">
              <Label>{field.label}</Label>
              <Input
                placeholder={field.placeholder}
                value={values[field.key] ?? ''}
                onChange={(e) => setValues({ ...values, [field.key]: e.target.value })}
              />
            </div>
          ))}
          <Button onClick={handleSubmit} disabled={loading} className="w-full">
            {loading ? 'Подключение...' : 'Подключить'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
```

**Step 3: Implement `app/(app)/integrations/page.tsx`**

```tsx
'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { PlatformCard } from '@/components/integrations/PlatformCard'
import { ConnectDialog } from '@/components/integrations/ConnectDialog'

const PLATFORMS = [
  { id: 'telegram', label: 'Telegram', description: 'Бот для канала и уведомлений', color: '#2AABEE' },
  { id: 'vk', label: 'ВКонтакте', description: 'Публикации и комментарии', color: '#4680C2' },
  { id: 'yandex_business', label: 'Яндекс.Бизнес', description: 'Отзывы и информация', color: '#FC3F1D' },
]

const DISABLED_PLATFORMS = [
  { id: '2gis', label: '2ГИС', description: 'Скоро (Phase 2)', color: '#1DA045' },
  { id: 'avito', label: 'Авито', description: 'Скоро (Phase 2)', color: '#00AAFF' },
  { id: 'google', label: 'Google Business', description: 'Скоро (Phase 3)', color: '#4285F4' },
]

export default function IntegrationsPage() {
  const qc = useQueryClient()
  const [connectingPlatform, setConnectingPlatform] = useState<string | null>(null)

  const { data } = useQuery({
    queryKey: ['integrations'],
    queryFn: () => api.get('/integrations').then((r) => r.data.integrations ?? []),
  })

  const disconnectMutation = useMutation({
    mutationFn: (platform: string) => api.delete(`/integrations/${platform}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['integrations'] }); toast.success('Отключено') },
    onError: () => toast.error('Ошибка отключения'),
  })

  const connectMutation = useMutation({
    mutationFn: ({ platform, credentials }: { platform: string; credentials: Record<string, string> }) =>
      api.post(`/integrations/${platform}/connect`, credentials),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['integrations'] }); toast.success('Подключено') },
    onError: () => toast.error('Ошибка подключения'),
  })

  const getStatus = (platformId: string) => {
    const found = data?.find((i: { platform: string; status: string }) => i.platform === platformId)
    return found?.status ?? null
  }

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-bold mb-6">Интеграции</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
        {PLATFORMS.map((p) => (
          <PlatformCard
            key={p.id}
            {...p}
            platform={p.id}
            status={getStatus(p.id)}
            onConnect={() => setConnectingPlatform(p.id)}
            onDisconnect={() => disconnectMutation.mutate(p.id)}
          />
        ))}
      </div>

      <h2 className="text-lg font-medium text-gray-400 mb-4">Скоро</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {DISABLED_PLATFORMS.map((p) => (
          <PlatformCard
            key={p.id}
            {...p}
            platform={p.id}
            status={null}
            disabled
            onConnect={() => {}}
            onDisconnect={() => {}}
          />
        ))}
      </div>

      {connectingPlatform && (
        <ConnectDialog
          platform={connectingPlatform}
          open={true}
          onClose={() => setConnectingPlatform(null)}
          onConnect={(credentials) =>
            connectMutation.mutateAsync({ platform: connectingPlatform, credentials })
          }
        />
      )}
    </div>
  )
}
```

**Step 4: Verify with dev server and run tests**

```bash
cd /Users/f1xgun/onevoice/services/frontend
pnpm test && pnpm build
```
Expected: all pass

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/
git commit -m "feat(frontend): add integrations page with platform cards and connect dialogs"
```

---

### Task 8: Dockerfile + nginx.conf

**Files:**
- Create: `services/frontend/Dockerfile`
- Create: `nginx/nginx.conf`
- Modify: `docker-compose.yml`

**Step 1: Create `services/frontend/Dockerfile`**

```dockerfile
FROM node:20-alpine AS deps
WORKDIR /app
RUN npm install -g pnpm
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM node:20-alpine AS builder
WORKDIR /app
RUN npm install -g pnpm
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
```

Add `output: 'standalone'` to `next.config.js`:

```js
const nextConfig = {
  output: 'standalone',
  async rewrites() { ... }
}
```

**Step 2: Create `nginx/nginx.conf`**

```nginx
events { worker_connections 1024; }

http {
  upstream api        { server api:8080; }
  upstream orchestrator { server orchestrator:8090; }
  upstream frontend   { server frontend:3000; }

  server {
    listen 80;

    location /api/v1/ {
      proxy_pass http://api;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
    }

    location /chat/ {
      proxy_pass http://orchestrator;
      proxy_set_header Host $host;
      proxy_http_version 1.1;
      proxy_set_header Connection '';      # SSE: keep connection open
      proxy_buffering off;
      proxy_cache off;
      chunked_transfer_encoding on;
    }

    location / {
      proxy_pass http://frontend;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
    }
  }
}
```

**Step 3: Add frontend + nginx to `docker-compose.yml`**

Add to the `services:` block:

```yaml
  frontend:
    build:
      context: ./services/frontend
      dockerfile: Dockerfile
    environment:
      - API_URL=http://api:8080
      - ORCHESTRATOR_URL=http://orchestrator:8090
    depends_on:
      - api
      - orchestrator

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/nginx.conf:ro
    depends_on:
      - frontend
      - api
      - orchestrator
```

**Step 4: Verify build**

```bash
cd /Users/f1xgun/onevoice/services/frontend && pnpm build
```
Expected: successful Next.js production build

**Step 5: Commit**

```bash
cd /Users/f1xgun/onevoice
git add services/frontend/Dockerfile nginx/ docker-compose.yml
git commit -m "feat(frontend): add Dockerfile and Nginx reverse proxy config"
```
