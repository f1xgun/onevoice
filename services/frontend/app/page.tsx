// Public marketing landing — OneVoice (Linen rebuild).
//
// Per design_handoff_onevoice 2/mocks/mock-landing.jsx + Brand Voice Guide:
// editorial typographic hero (no gradient hero, no stock photos), serif
// italic ochre wordmark inside the headline, an inline composed inbox
// preview as the hero visual, four alternating feature rows with embedded
// UI samples, three-step how-it-works, 10-up channels grid, pull quote,
// single-tier pricing CTA, footer. All copy passes the brand voice rules:
// no exclamation marks, no emoji, no AI-powered hype, no urgency tactics.

import Link from 'next/link';
import { ArrowRight, Calendar } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';

// ─── Local helpers ────────────────────────────────────────────────────
// The wordmark is the only place we use a serif on the landing — inline
// font-family avoids loading a webfont just for one phrase.
const SERIF = '"Iowan Old Style", "Georgia", "Times New Roman", serif';

const NAV_LINKS = [
  { href: '#features', label: 'Возможности' },
  { href: '#channels', label: 'Каналы' },
  { href: '#pricing', label: 'Цены' },
];

const PLATFORMS: Array<{
  name: string;
  display: string;
  meta: string;
  status: 'есть' | 'скоро' | 'API';
}> = [
  { name: 'Telegram', display: 'Telegram', meta: 'Каналы и боты', status: 'есть' },
  { name: 'VK', display: 'ВКонтакте', meta: 'Сообщества', status: 'есть' },
  { name: 'Yandex', display: 'Яндекс.Бизнес', meta: 'Карта и отзывы', status: 'есть' },
  { name: 'Google', display: 'Google Business', meta: 'Карта и отзывы', status: 'скоро' },
  { name: '2GIS', display: '2ГИС', meta: 'Карта и отзывы', status: 'скоро' },
  { name: 'Avito', display: 'Авито', meta: 'Объявления', status: 'скоро' },
  { name: 'WhatsApp', display: 'WhatsApp', meta: 'Оценивается', status: 'скоро' },
  { name: 'Instagram', display: 'Instagram', meta: 'Оценивается', status: 'скоро' },
  { name: 'OK', display: 'Одноклассники', meta: 'Оценивается', status: 'скоро' },
  { name: 'OneVoice', display: 'Свой канал', meta: 'Через API', status: 'API' },
];

const STEPS = [
  {
    n: '01',
    t: 'Подключите',
    d: 'Telegram, ВКонтакте, Яндекс.Бизнес — по одной ссылке за раз. Без ключей API и тонких настроек.',
  },
  {
    n: '02',
    t: 'Поговорите',
    d: 'Расскажите про меню, часы работы и тон. OneVoice запомнит и будет писать вашими словами.',
  },
  {
    n: '03',
    t: 'Получайте',
    d: 'Ответы клиентам, посты на все каналы, отчёт по отзывам. Вы — последнее слово, всегда.',
  },
];

// ─── Page ─────────────────────────────────────────────────────────────

export default function LandingPage() {
  return (
    <div className="min-h-screen bg-paper text-ink">
      <SiteNav />
      <Hero />
      <Belief />
      <Features />
      <HowItWorks />
      <Platforms />
      <Quote />
      <Pricing />
      <SiteFooter />
    </div>
  );
}

// ─── Nav ──────────────────────────────────────────────────────────────

function SiteNav() {
  return (
    <header className="sticky top-0 z-10 border-b border-line-soft bg-paper/85 backdrop-blur">
      <div className="mx-auto flex h-16 w-full max-w-[1180px] items-center gap-8 px-6 sm:px-12">
        <Link href="/" className="flex items-center gap-2 text-[15px] font-semibold tracking-tight">
          <span className="inline-flex h-[26px] w-[26px] items-center justify-center rounded-md bg-ink text-[11px] font-semibold text-paper">
            OV
          </span>
          OneVoice
        </Link>
        <nav className="hidden items-center gap-6 text-sm text-ink-mid md:flex">
          {NAV_LINKS.map((l) => (
            <a
              key={l.href}
              href={l.href}
              className="transition-colors hover:text-ink"
            >
              {l.label}
            </a>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <Link
            href="/login"
            className="text-sm text-ink-mid transition-colors hover:text-ink"
          >
            Войти
          </Link>
          <Button asChild size="sm" variant="primary">
            <Link href="/register">Попробовать</Link>
          </Button>
        </div>
      </div>
    </header>
  );
}

// ─── Hero ─────────────────────────────────────────────────────────────

function Hero() {
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto grid w-full max-w-[1180px] items-center gap-16 px-6 py-20 sm:px-12 md:py-28 lg:grid-cols-[1.1fr_0.9fr]">
        <div>
          <MonoLabel>One Voice · v 0.9 · открытая бета</MonoLabel>
          <h1
            className="mt-5 text-[44px] font-medium leading-[1.04] tracking-[-0.025em] text-pretty sm:text-[56px] lg:text-[64px]"
          >
            Один разговор<br />
            для всех каналов{' '}
            <span
              className="font-normal italic text-ochre-deep"
              style={{ fontFamily: SERIF }}
            >
              OneVoice
            </span>
            .
          </h1>
          <p className="mt-6 max-w-[520px] text-[17px] leading-relaxed text-ink-mid sm:text-lg">
            Telegram, ВКонтакте, Яндекс.Бизнес — в одном ящике. OneVoice пишет
            черновики ответов, готовит посты для всех каналов и держит отзывы
            на виду, пока вы заняты делом.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Button asChild size="lg" variant="primary">
              <Link href="/register">
                Попробовать
                <ArrowRight aria-hidden />
              </Link>
            </Button>
            <Button asChild size="lg" variant="secondary">
              <a href="#features">Посмотреть, как работает</a>
            </Button>
          </div>
          <p className="mt-6 text-[13px] text-ink-soft">
            Без карты · 14 дней пробного периода · до 3 каналов
          </p>
        </div>
        <HeroPreview />
      </div>
    </section>
  );
}

function HeroPreview() {
  return (
    <div
      aria-hidden
      className="rounded-xl border border-line bg-paper-raised p-4 shadow-ov-3"
    >
      {/* Window chrome */}
      <div className="flex items-center gap-2 px-1.5 pb-3 pt-1">
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <MonoLabel className="ml-auto">onevoice.app/inbox</MonoLabel>
      </div>

      {/* Inbox card */}
      <div className="overflow-hidden rounded-md border border-line-soft bg-paper">
        <div className="flex items-center gap-2.5 border-b border-line-soft px-4 py-3">
          <span className="size-2 rounded-full bg-ochre" />
          <span className="text-[13px] font-semibold">Сегодня</span>
          <MonoLabel className="ml-auto">3 ждут ответа</MonoLabel>
        </div>

        {INBOX_ROWS.map((r, i) => (
          <div
            key={r.name}
            className={`grid items-center gap-3 px-4 py-3 sm:grid-cols-[88px_1fr_auto] ${
              i < INBOX_ROWS.length - 1 ? 'border-b border-line-soft' : ''
            }`}
          >
            <div className="flex items-center gap-2">
              <ChannelMark name={r.channel} size={20} />
              <span className="text-[12px] text-ink-soft">{r.channelLabel}</span>
            </div>
            <div className="min-w-0">
              <div className="truncate text-[13px] font-medium">{r.name}</div>
              <div className="mt-0.5 truncate text-[13px] text-ink-mid">{r.msg}</div>
            </div>
            <Badge tone={r.tone}>{r.label}</Badge>
          </div>
        ))}

        {/* Tool-call composing line */}
        <div className="flex items-center gap-2.5 bg-paper-sunken px-4 py-3">
          <span className="size-2 rounded-full bg-ochre" />
          <span className="text-[13px] text-ink">
            OneVoice готовит черновик для Алёны
          </span>
          <MonoLabel className="ml-auto">~4 сек</MonoLabel>
        </div>
      </div>
    </div>
  );
}

const INBOX_ROWS: Array<{
  channel: string;
  channelLabel: string;
  name: string;
  msg: string;
  tone: 'accent' | 'success' | 'neutral';
  label: string;
}> = [
  {
    channel: 'Telegram',
    channelLabel: 'Telegram',
    name: 'Алёна К.',
    msg: 'Вы открыты в воскресенье?',
    tone: 'accent',
    label: 'черновик готов',
  },
  {
    channel: 'Yandex',
    channelLabel: 'Яндекс',
    name: 'Михаил П.',
    msg: 'Поставил пять звёзд — спасибо за внимание',
    tone: 'success',
    label: 'отвечено',
  },
  {
    channel: 'VK',
    channelLabel: 'VK',
    name: 'Olga · комментарий',
    msg: 'А капучино у вас на овсяном делают?',
    tone: 'neutral',
    label: 'новое',
  },
];

// ─── Belief ───────────────────────────────────────────────────────────

function Belief() {
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-20 sm:px-12">
        <MonoLabel>Как мы это видим</MonoLabel>
        <h2 className="mt-3 max-w-[880px] text-[28px] font-medium leading-[1.18] tracking-[-0.015em] text-pretty sm:text-[34px]">
          Малый бизнес проигрывает не потому, что плохо работает. А потому, что{' '}
          <span
            className="font-normal italic text-ochre-deep"
            style={{ fontFamily: SERIF }}
          >
            не успевает отвечать.
          </span>
        </h2>
        <p className="mt-5 max-w-[720px] text-[17px] leading-relaxed text-ink-mid">
          Кофейня, салон, мастерская — десять каналов и одна пара рук.
          OneVoice не заменяет вас. Он отвечает первым, чтобы у вас осталось
          время ответить лучше.
        </p>
      </div>
    </section>
  );
}

// ─── Features ─────────────────────────────────────────────────────────

function Features() {
  const items: Array<{
    kicker: string;
    title: string;
    body: string;
    sample: React.ReactNode;
  }> = [
    {
      kicker: '01 · Один ящик',
      title: 'Все каналы — одним списком',
      body: 'Сообщения, отзывы и комментарии из Telegram, ВКонтакте и Яндекс.Бизнес собираются в общий поток. Без переключений и забытых вкладок.',
      sample: <SampleInbox />,
    },
    {
      kicker: '02 · Черновики',
      title: 'Ответ за пару секунд, ваш — за пару минут',
      body: 'OneVoice знает ваши часы работы, меню и тон. Готовит черновик. Вы отправляете, правите или просите попробовать ещё раз.',
      sample: <SampleDraft />,
    },
    {
      kicker: '03 · Посты',
      title: 'Один пост — все каналы сразу',
      body: 'Напишите один раз — публикуется везде, с поправкой на формат каждой площадки. Запланируйте на час, когда вас читают.',
      sample: <SamplePosts />,
    },
    {
      kicker: '04 · Отзывы',
      title: 'Отзывы под спокойным присмотром',
      body: 'Каждый отзыв на виду. Низкие оценки — наверху списка. Высокие — с благодарностью в вашем тоне.',
      sample: <SampleReviews />,
    },
  ];

  return (
    <section id="features" className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>Возможности</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[32px] font-medium leading-tight tracking-[-0.015em] sm:text-[40px]">
          Четыре вещи, на которые у вас обычно нет времени.
        </h2>

        <div className="mt-12 grid grid-cols-1 gap-10 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-14">
          {items.map((it) => (
            <article
              key={it.kicker}
              className="flex flex-col gap-6 border-t border-line pt-8"
            >
              <div>
                <MonoLabel tone="ochre">{it.kicker}</MonoLabel>
                <h3 className="mt-2 text-[24px] font-medium leading-snug tracking-[-0.01em]">
                  {it.title}
                </h3>
                <p className="mt-3 max-w-[460px] text-[15px] leading-relaxed text-ink-mid">
                  {it.body}
                </p>
              </div>
              {it.sample}
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

const sampleBox = 'overflow-hidden rounded-lg border border-line bg-paper-raised';

function SampleInbox() {
  const rows = [
    { p: 'Telegram', label: 'Telegram', msg: 'Вы открыты завтра?', t: '1 мин' },
    { p: 'VK', label: 'VK', msg: 'Где вас найти?', t: '12 мин' },
    { p: 'Yandex', label: 'Яндекс', msg: 'Спасибо, всё на месте — пять звёзд', t: '1 ч' },
  ];
  return (
    <div className={sampleBox} aria-hidden>
      {rows.map((r, i) => (
        <div
          key={r.p}
          className={`grid items-center gap-3 px-4 py-3 sm:grid-cols-[96px_1fr_auto] ${
            i > 0 ? 'border-t border-line-soft' : ''
          }`}
        >
          <div className="flex items-center gap-2">
            <ChannelMark name={r.p} size={20} />
            <span className="text-[12px] text-ink-soft">{r.label}</span>
          </div>
          <div className="truncate text-[13px] text-ink-mid">{r.msg}</div>
          <MonoLabel>{r.t}</MonoLabel>
        </div>
      ))}
    </div>
  );
}

function SampleDraft() {
  return (
    <div className={sampleBox} aria-hidden>
      <div className="flex flex-col gap-3 p-4">
        <div className="flex items-center gap-2.5">
          <ChannelMark name="Telegram" size={20} />
          <span className="text-[13px] text-ink-mid">от: Алёна К.</span>
          <MonoLabel className="ml-auto">4 мин назад</MonoLabel>
        </div>
        <div className="rounded-md bg-paper-sunken px-3.5 py-2.5 text-[13px]">
          Вы открыты в воскресенье?
        </div>
        <div className="rounded-md border border-ochre-soft bg-ochre-soft/60 px-3.5 py-3 text-[13px] text-ink">
          Да, по воскресеньям мы работаем с 10:00 до 20:00 — будем рады вас видеть.
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" variant="primary">
              Отправить
            </Button>
            <Button size="sm" variant="secondary">
              Поправить
            </Button>
            <Button size="sm" variant="ghost">
              Ещё вариант
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function SamplePosts() {
  return (
    <div className={sampleBox} aria-hidden>
      <div className="flex flex-col gap-3 p-4">
        <div className="rounded-md bg-paper-sunken px-3.5 py-2.5 text-[13px]">
          Завтра до 11 утра — все капучино за 200 ₽. Заходите.
        </div>
        <div className="flex items-center gap-2">
          <MonoLabel>опубликовать в:</MonoLabel>
          <ChannelMark name="Telegram" size={20} />
          <ChannelMark name="VK" size={20} />
          <ChannelMark name="Yandex" size={20} />
        </div>
        <div className="flex items-center gap-3 border-t border-line-soft pt-3">
          <span className="inline-flex items-center gap-1.5 text-[13px] text-ink-mid">
            <Calendar className="size-3.5" aria-hidden />
            завтра, 09:00
          </span>
          <span className="ml-auto">
            <Button size="sm" variant="primary">
              Запланировать
            </Button>
          </span>
        </div>
      </div>
    </div>
  );
}

function SampleReviews() {
  const items: Array<{
    stars: number;
    name: string;
    text: string;
    plat: string;
    replied: boolean;
  }> = [
    {
      stars: 5,
      name: 'Михаил П.',
      text: 'Лучший флэт в районе. Вернусь.',
      plat: 'Yandex',
      replied: true,
    },
    {
      stars: 3,
      name: 'Анна',
      text: 'Капучино обычный, но обслуживание вежливое.',
      plat: 'Yandex',
      replied: false,
    },
  ];
  return (
    <div className={sampleBox} aria-hidden>
      {items.map((r, i) => (
        <div
          key={r.name}
          className={`px-4 py-3 ${i > 0 ? 'border-t border-line-soft' : ''}`}
        >
          <div className="mb-1.5 flex items-center gap-2">
            <span className="text-[13px] tracking-[1px] text-ochre">
              {'★'.repeat(r.stars)}
              <span className="text-line">{'★'.repeat(5 - r.stars)}</span>
            </span>
            <span className="text-[13px] font-medium">{r.name}</span>
            <span className="ml-auto">
              <ChannelMark name={r.plat} size={20} />
            </span>
          </div>
          <div className="text-[13px] text-ink-mid">{r.text}</div>
          <div className="mt-2">
            {r.replied ? (
              <Badge tone="success">отвечено OneVoice</Badge>
            ) : (
              <Badge tone="warning">черновик готов · нужно ваше «да»</Badge>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

// ─── How it works ────────────────────────────────────────────────────

function HowItWorks() {
  return (
    <section className="border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>Как это работает</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          Три шага. Полчаса вашего времени.
        </h2>
        <div className="mt-14 grid grid-cols-1 gap-10 sm:grid-cols-3">
          {STEPS.map((s) => (
            <div key={s.n}>
              <span
                className="font-mono text-[15px] font-medium tracking-[0.04em] text-ochre-deep"
                aria-hidden
              >
                {s.n}
              </span>
              <h3 className="mt-3 text-[22px] font-medium tracking-[-0.005em]">
                {s.t}
              </h3>
              <p className="mt-2.5 text-[15px] leading-relaxed text-ink-mid">
                {s.d}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ─── Channels ─────────────────────────────────────────────────────────

function Platforms() {
  return (
    <section id="channels" className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>Каналы</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          Поддерживаем то, чем пользуются ваши клиенты.
        </h2>
        <p className="mt-3 max-w-[640px] text-[16px] leading-relaxed text-ink-mid">
          В России — без компромиссов. Подключаем настоящие каналы, а не их
          западные аналоги.
        </p>
        <div className="mt-12 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          {PLATFORMS.map((p) => {
            const tone = p.status === 'есть' ? 'success' : p.status === 'API' ? 'info' : 'neutral';
            return (
              <div
                key={p.display}
                className="flex min-h-[120px] flex-col gap-3 rounded-lg border border-line bg-paper-raised p-4"
              >
                <div className="flex items-center justify-between">
                  <ChannelMark name={p.name} size={28} />
                  <Badge tone={tone}>{p.status}</Badge>
                </div>
                <div className="mt-auto">
                  <div className="text-[14px] font-medium">{p.display}</div>
                  <MonoLabel>{p.meta}</MonoLabel>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}

// ─── Quote ────────────────────────────────────────────────────────────

function Quote() {
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[880px] px-6 py-28 sm:px-12">
        <MonoLabel>Из писем</MonoLabel>
        <blockquote
          className="mt-6 text-[26px] font-normal leading-snug tracking-[-0.015em] text-ink text-pretty sm:text-[32px]"
          style={{ fontFamily: SERIF, fontStyle: 'italic' }}
        >
          «Раньше я открывала пять вкладок, чтобы понять, где у меня сегодня
          пожар. Теперь смотрю в один ящик — и вижу, что OneVoice уже ответил
          на половину, а с другой половиной просит мою подпись.»
        </blockquote>
        <div className="mt-7 flex items-center gap-3">
          <ChannelMark name="You" size={36} />
          <div>
            <div className="text-[14px] font-medium">Татьяна Б.</div>
            <MonoLabel>Кофейня «Мята», Москва · с нами 6 месяцев</MonoLabel>
          </div>
        </div>
      </div>
    </section>
  );
}

// ─── Pricing CTA ──────────────────────────────────────────────────────

function Pricing() {
  return (
    <section id="pricing">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-28 sm:px-12">
        <div className="grid items-center gap-12 rounded-xl border border-line bg-paper-raised p-10 sm:p-14 lg:grid-cols-[1.4fr_1fr] lg:gap-14">
          <div>
            <MonoLabel>Тариф</MonoLabel>
            <h2 className="mt-3 text-[28px] font-medium leading-[1.18] tracking-[-0.015em] sm:text-[34px]">
              Подключите первый канал за минуту. Остальное — наша забота.
            </h2>
            <p className="mt-4 max-w-[480px] text-[16px] leading-relaxed text-ink-mid">
              14 дней бесплатно. Без карты. До трёх каналов и без лимита на
              сообщения и посты.
            </p>
            <div className="mt-7 flex flex-wrap gap-3">
              <Button asChild size="lg" variant="primary">
                <Link href="/register">
                  Попробовать бесплатно
                  <ArrowRight aria-hidden />
                </Link>
              </Button>
              <Button asChild size="lg" variant="ghost">
                <a href="mailto:hello@onevoice.app">Поговорить с командой</a>
              </Button>
            </div>
          </div>
          <div className="rounded-lg border border-line bg-paper-sunken p-6">
            <MonoLabel>Тариф «Одна точка»</MonoLabel>
            <div className="mt-2 flex items-baseline gap-1.5">
              <span className="text-[40px] font-medium tracking-[-0.02em] sm:text-[44px]">
                1 490
              </span>
              <span className="text-[16px] text-ink-mid">₽ / мес</span>
            </div>
            <ul className="mt-4 flex flex-col gap-2 text-[14px] text-ink-mid">
              {[
                'До 3 каналов',
                'Без лимита на сообщения и посты',
                'История за 12 месяцев',
                'Поддержка в Telegram',
              ].map((l) => (
                <li key={l} className="flex items-center gap-2">
                  <span className="inline-flex size-3.5 items-center justify-center rounded-full bg-ochre-soft text-[10px] text-ochre-ink">
                    ✓
                  </span>
                  {l}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </section>
  );
}

// ─── Footer ──────────────────────────────────────────────────────────

function SiteFooter() {
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex w-full max-w-[1180px] flex-wrap items-center gap-6 px-6 py-10 text-[13px] text-ink-soft sm:px-12">
        <span className="flex items-center gap-2 font-semibold text-ink">
          <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-ink text-[10px] text-paper">
            OV
          </span>
          OneVoice
        </span>
        <MonoLabel>© {new Date().getFullYear()} · Все права защищены</MonoLabel>
        <div className="ml-auto flex flex-wrap items-center gap-5">
          <a href="#" className="transition-colors hover:text-ink">
            Условия
          </a>
          <a href="#" className="transition-colors hover:text-ink">
            Конфиденциальность
          </a>
          <a href="mailto:hello@onevoice.app" className="transition-colors hover:text-ink">
            Контакты
          </a>
        </div>
      </div>
    </footer>
  );
}
