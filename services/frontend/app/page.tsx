import Link from 'next/link';
import {
  MessageSquare,
  Star,
  Share2,
  Zap,
  ArrowRight,
} from 'lucide-react';
import { Button } from '@/components/ui/button';

const features = [
  {
    icon: MessageSquare,
    title: 'ИИ-чат',
    description: 'Общайтесь с ИИ-ассистентом для управления вашим бизнесом',
  },
  {
    icon: Share2,
    title: 'Мультиплатформенность',
    description: 'Telegram, VK, Яндекс Бизнес — всё в одном месте',
  },
  {
    icon: Star,
    title: 'Управление отзывами',
    description: 'Автоматические ответы на отзывы с помощью ИИ',
  },
  {
    icon: Zap,
    title: 'Автоматизация',
    description: 'Публикация постов, управление каналами и многое другое',
  },
];

const platforms = [
  { name: 'Telegram', color: '#2AABEE' },
  { name: 'VK', color: '#0077FF' },
  { name: 'Яндекс Бизнес', color: '#FC3F1D' },
];

const steps = [
  { step: '01', title: 'Создайте бизнес', description: 'Заполните профиль вашей компании' },
  { step: '02', title: 'Подключите платформы', description: 'Свяжите каналы в один клик' },
  { step: '03', title: 'Управляйте через ИИ', description: 'Чат-ассистент сделает остальное' },
];

export default function LandingPage() {
  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-950 via-gray-900 to-gray-950 text-white">
      {/* Hero */}
      <header className="relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top,rgba(99,102,241,0.15),transparent_60%)]" />
        <nav className="relative mx-auto flex max-w-6xl items-center justify-between px-6 py-6">
          <span className="text-xl font-bold tracking-tight">OneVoice</span>
          <div className="flex items-center gap-3">
            <Link href="/login">
              <Button variant="ghost" className="text-white hover:text-white/80">
                Войти
              </Button>
            </Link>
            <Link href="/register">
              <Button className="bg-indigo-600 hover:bg-indigo-700">Начать</Button>
            </Link>
          </div>
        </nav>

        <div className="relative mx-auto max-w-3xl px-6 pb-24 pt-20 text-center">
          <h1 className="mb-6 text-4xl font-bold leading-tight tracking-tight sm:text-5xl lg:text-6xl">
            Одна платформа для{' '}
            <span className="bg-gradient-to-r from-indigo-400 to-blue-400 bg-clip-text text-transparent">
              всего бизнеса
            </span>
          </h1>
          <p className="mx-auto mb-8 max-w-xl text-lg text-gray-400">
            Управляйте Telegram, VK и Яндекс Бизнес через единый ИИ-интерфейс.
            Автоматизируйте посты, отзывы и коммуникации.
          </p>
          <Link href="/register">
            <Button size="lg" className="bg-indigo-600 hover:bg-indigo-700">
              Начать бесплатно
              <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </Link>
        </div>
      </header>

      {/* Features */}
      <section className="mx-auto max-w-5xl px-6 py-20">
        <h2 className="mb-12 text-center text-3xl font-bold">Возможности</h2>
        <div className="grid gap-6 sm:grid-cols-2">
          {features.map((f) => (
            <div
              key={f.title}
              className="rounded-xl border border-gray-800 bg-gray-900/50 p-6"
            >
              <f.icon className="mb-3 h-8 w-8 text-indigo-400" />
              <h3 className="mb-2 text-lg font-semibold">{f.title}</h3>
              <p className="text-sm text-gray-400">{f.description}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Platforms */}
      <section className="border-y border-gray-800 bg-gray-900/30 py-16">
        <div className="mx-auto max-w-4xl px-6 text-center">
          <h2 className="mb-8 text-2xl font-bold">Поддерживаемые платформы</h2>
          <div className="flex flex-wrap items-center justify-center gap-8">
            {platforms.map((p) => (
              <div key={p.name} className="flex items-center gap-3">
                <div
                  className="h-3 w-3 rounded-full"
                  style={{ backgroundColor: p.color }}
                />
                <span className="text-lg font-medium text-gray-300">{p.name}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* How it works */}
      <section className="mx-auto max-w-4xl px-6 py-20">
        <h2 className="mb-12 text-center text-3xl font-bold">Как это работает</h2>
        <div className="grid gap-8 sm:grid-cols-3">
          {steps.map((s) => (
            <div key={s.step} className="text-center">
              <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-indigo-600/20 text-lg font-bold text-indigo-400">
                {s.step}
              </div>
              <h3 className="mb-2 font-semibold">{s.title}</h3>
              <p className="text-sm text-gray-400">{s.description}</p>
            </div>
          ))}
        </div>
      </section>

      {/* CTA */}
      <section className="mx-auto max-w-3xl px-6 pb-20 text-center">
        <div className="rounded-2xl border border-gray-800 bg-gradient-to-br from-indigo-950/50 to-gray-900 p-12">
          <h2 className="mb-4 text-2xl font-bold">Готовы начать?</h2>
          <p className="mb-6 text-gray-400">
            Создайте аккаунт за 30 секунд и подключите первую платформу.
          </p>
          <Link href="/register">
            <Button size="lg" className="bg-indigo-600 hover:bg-indigo-700">
              Создать аккаунт
              <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </Link>
        </div>
      </section>

      {/* Footer */}
      <footer className="border-t border-gray-800 py-8">
        <div className="mx-auto max-w-6xl px-6 text-center text-sm text-gray-500">
          &copy; {new Date().getFullYear()} OneVoice. Все права защищены.
        </div>
      </footer>
    </div>
  );
}
