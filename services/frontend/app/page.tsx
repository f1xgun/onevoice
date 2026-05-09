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
import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ChannelMark } from '@/components/ui/channel-mark';
import { MonoLabel } from '@/components/ui/mono-label';
import { SupportedPlatforms } from '@/components/landing/SupportedPlatforms';

// ─── Local helpers ────────────────────────────────────────────────────
// The wordmark is the only place we use a serif on the landing — inline
// font-family avoids loading a webfont just for one phrase.
const SERIF = '"Iowan Old Style", "Georgia", "Times New Roman", serif';

// 5-star scale used by the inline review-preview block in the hero.
const MAX_REVIEW_STARS = 5;

// Static href anchors for nav. Labels are resolved via useTranslations
// inside each component.
const NAV_HREFS = ['#features', '#channels'] as const;

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
      {/* <Pricing /> — временно скрыто: продукт бесплатный */}
      <SiteFooter />
    </div>
  );
}

// ─── Nav ──────────────────────────────────────────────────────────────

function SiteNav() {
  const tNav = useTranslations('landing.nav');
  const navLabels: Record<(typeof NAV_HREFS)[number], string> = {
    '#features': tNav('features'),
    '#channels': tNav('channels'),
  };
  return (
    <header className="bg-paper/85 sticky top-0 z-10 border-b border-line-soft backdrop-blur">
      <div className="mx-auto flex h-16 w-full max-w-[1180px] items-center gap-8 px-6 sm:px-12">
        <Link href="/" className="flex items-center gap-2 text-[15px] font-semibold tracking-tight">
          <span className="inline-flex h-[26px] w-[26px] items-center justify-center rounded-md bg-ink text-[11px] font-semibold text-paper">
            {tNav('logoMark')}
          </span>
          {tNav('wordmark')}
        </Link>
        <nav className="hidden items-center gap-6 text-sm text-ink-mid md:flex">
          {NAV_HREFS.map((href) => (
            <a key={href} href={href} className="transition-colors hover:text-ink">
              {navLabels[href]}
            </a>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <Link href="/login" className="text-sm text-ink-mid transition-colors hover:text-ink">
            {tNav('login')}
          </Link>
          <Button asChild size="sm" variant="primary">
            <Link href="/register">{tNav('tryIt')}</Link>
          </Button>
        </div>
      </div>
    </header>
  );
}

// ─── Hero ─────────────────────────────────────────────────────────────

function Hero() {
  const tHero = useTranslations('landing.hero');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto grid w-full max-w-[1180px] items-center gap-16 px-6 py-20 sm:px-12 md:py-28 lg:grid-cols-[1.1fr_0.9fr]">
        <div>
          <MonoLabel>{tHero('kicker')}</MonoLabel>
          <h1 className="mt-5 text-pretty text-[44px] font-medium leading-[1.04] tracking-[-0.025em] sm:text-[56px] lg:text-[64px]">
            {tHero('headlineLine1')}
            <br />
            {tHero('headlineLine2Prefix')}
            <span className="font-normal italic text-ochre-deep" style={{ fontFamily: SERIF }}>
              {tHero('wordmark')}
            </span>
            {tHero('headlinePunctuation')}
          </h1>
          <p className="mt-6 max-w-[520px] text-[17px] leading-relaxed text-ink-mid sm:text-lg">
            {tHero('body')}
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Button asChild size="lg" variant="primary">
              <Link href="/register">
                {tHero('cta')}
                <ArrowRight aria-hidden />
              </Link>
            </Button>
            <Button asChild size="lg" variant="secondary">
              <a href="#features">{tHero('secondary')}</a>
            </Button>
          </div>
        </div>
        <HeroPreview />
      </div>
    </section>
  );
}

function HeroPreview() {
  const tPreview = useTranslations('landing.preview');
  return (
    <div aria-hidden className="rounded-xl border border-line bg-paper-raised p-4 shadow-ov-3">
      {/* Window chrome */}
      <div className="flex items-center gap-2 px-1.5 pb-3 pt-1">
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <span className="size-[10px] rounded-full border border-line bg-paper-sunken" />
        <MonoLabel className="ml-auto">{tPreview('url')}</MonoLabel>
      </div>

      {/* Inbox card */}
      <div className="overflow-hidden rounded-md border border-line-soft bg-paper">
        <div className="flex items-center gap-2.5 border-b border-line-soft px-4 py-3">
          <span className="size-2 rounded-full bg-ochre" />
          <span className="text-[13px] font-semibold">{tPreview('today')}</span>
          <MonoLabel className="ml-auto">{tPreview('waiting')}</MonoLabel>
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
          <span className="text-[13px] text-ink">{tPreview('composing')}</span>
          <MonoLabel className="ml-auto">{tPreview('eta')}</MonoLabel>
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
  const tBelief = useTranslations('landing.belief');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-20 sm:px-12">
        <MonoLabel>{tBelief('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[880px] text-pretty text-[28px] font-medium leading-[1.18] tracking-[-0.015em] sm:text-[34px]">
          {tBelief('headlinePrefix')}
          <span className="font-normal italic text-ochre-deep" style={{ fontFamily: SERIF }}>
            {tBelief('headlineHighlight')}
          </span>
        </h2>
        <p className="mt-5 max-w-[720px] text-[17px] leading-relaxed text-ink-mid">
          {tBelief('body')}
        </p>
      </div>
    </section>
  );
}

// ─── Features ─────────────────────────────────────────────────────────

function Features() {
  const tF = useTranslations('landing.features');
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
        <MonoLabel>{tF('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[32px] font-medium leading-tight tracking-[-0.015em] sm:text-[40px]">
          {tF('headline')}
        </h2>

        <div className="mt-12 grid grid-cols-1 gap-10 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-14">
          {items.map((it) => (
            <article key={it.kicker} className="flex flex-col gap-6 border-t border-line pt-8">
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
  const tS = useTranslations('landing.sample');
  return (
    <div className={sampleBox} aria-hidden>
      <div className="flex flex-col gap-3 p-4">
        <div className="flex items-center gap-2.5">
          <ChannelMark name="Telegram" size={20} />
          <span className="text-[13px] text-ink-mid">{tS('from')}</span>
          <MonoLabel className="ml-auto">{tS('ago')}</MonoLabel>
        </div>
        <div className="rounded-md bg-paper-sunken px-3.5 py-2.5 text-[13px]">{tS('question')}</div>
        <div className="bg-ochre-soft/60 rounded-md border border-ochre-soft px-3.5 py-3 text-[13px] text-ink">
          {tS('answer')}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" variant="primary">
              {tS('send')}
            </Button>
            <Button size="sm" variant="secondary">
              {tS('fix')}
            </Button>
            <Button size="sm" variant="ghost">
              {tS('tryAgain')}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function SamplePosts() {
  const tS = useTranslations('landing.sample');
  return (
    <div className={sampleBox} aria-hidden>
      <div className="flex flex-col gap-3 p-4">
        <div className="rounded-md bg-paper-sunken px-3.5 py-2.5 text-[13px]">
          {tS('postContent')}
        </div>
        <div className="flex items-center gap-2">
          <MonoLabel>{tS('publishTo')}</MonoLabel>
          <ChannelMark name="Telegram" size={20} />
          <ChannelMark name="VK" size={20} />
          <ChannelMark name="Yandex" size={20} />
        </div>
        <div className="flex items-center gap-3 border-t border-line-soft pt-3">
          <span className="inline-flex items-center gap-1.5 text-[13px] text-ink-mid">
            <Calendar className="size-3.5" aria-hidden />
            {tS('scheduledTime')}
          </span>
          <span className="ml-auto">
            <Button size="sm" variant="primary">
              {tS('schedule')}
            </Button>
          </span>
        </div>
      </div>
    </div>
  );
}

function SampleReviews() {
  const tS = useTranslations('landing.sample');
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
        <div key={r.name} className={`px-4 py-3 ${i > 0 ? 'border-t border-line-soft' : ''}`}>
          <div className="mb-1.5 flex items-center gap-2">
            <span className="text-[13px] tracking-[1px] text-ochre">
              {'★'.repeat(r.stars)}
              <span className="text-line">{'★'.repeat(MAX_REVIEW_STARS - r.stars)}</span>
            </span>
            <span className="text-[13px] font-medium">{r.name}</span>
            <span className="ml-auto">
              <ChannelMark name={r.plat} size={20} />
            </span>
          </div>
          <div className="text-[13px] text-ink-mid">{r.text}</div>
          <div className="mt-2">
            {r.replied ? (
              <Badge tone="success">{tS('repliedByOneVoice')}</Badge>
            ) : (
              <Badge tone="warning">{tS('draftReady')}</Badge>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

// ─── How it works ────────────────────────────────────────────────────

function HowItWorks() {
  const tHow = useTranslations('landing.howItWorks');
  // Step copy is intentionally kept inline here — they're brand-voice
  // marketing strings that should stay close to the layout. The kicker /
  // headline use the i18n catalogue; per-step bullets remain untranslated.
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
  return (
    <section className="border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>{tHow('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          {tHow('headline')}
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
              <h3 className="mt-3 text-[22px] font-medium tracking-[-0.005em]">{s.t}</h3>
              <p className="mt-2.5 text-[15px] leading-relaxed text-ink-mid">{s.d}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

// ─── Channels ─────────────────────────────────────────────────────────

function Platforms() {
  const tCh = useTranslations('landing.channels');
  return (
    <section id="channels" className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>{tCh('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          {tCh('headline')}
        </h2>
        <p className="mt-3 max-w-[640px] text-[16px] leading-relaxed text-ink-mid">{tCh('body')}</p>
        <SupportedPlatforms />
      </div>
    </section>
  );
}

// ─── Quote ────────────────────────────────────────────────────────────

function Quote() {
  const t = useTranslations('landing.quote');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[880px] px-6 py-28 sm:px-12">
        <MonoLabel>{t('label')}</MonoLabel>
        <p
          className="mt-6 text-pretty text-[26px] font-normal leading-snug tracking-[-0.015em] text-ink sm:text-[32px]"
          style={{ fontFamily: SERIF, fontStyle: 'italic' }}
        >
          {t('body')}
        </p>
        <p className="mt-7 text-[13px] text-ink-soft">{t('note')}</p>
      </div>
    </section>
  );
}

// ─── Pricing CTA ──────────────────────────────────────────────────────
// Временно отключено: продукт бесплатный. Когда вернём оплату — раскомментировать
// <Pricing /> в LandingPage и пункт «Цены» в NAV_LINKS.
/*
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
*/

// ─── Footer ──────────────────────────────────────────────────────────

function SiteFooter() {
  const tFooter = useTranslations('landing.footer');
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex w-full max-w-[1180px] flex-wrap items-center gap-6 px-6 py-10 text-[13px] text-ink-soft sm:px-12">
        <span className="flex items-center gap-2 font-semibold text-ink">
          <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-ink text-[10px] text-paper">
            {tFooter('logoMark')}
          </span>
          {tFooter('wordmark')}
        </span>
        <MonoLabel>{tFooter('rights', { year: new Date().getFullYear() })}</MonoLabel>
        <div className="ml-auto flex flex-wrap items-center gap-5">
          <a href="mailto:hello@onevoice.app" className="transition-colors hover:text-ink">
            {tFooter('contacts')}
          </a>
        </div>
      </div>
    </footer>
  );
}
