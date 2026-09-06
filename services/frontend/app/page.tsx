import Link from 'next/link';
import { ArrowRight, Calendar, Check } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Badge } from '@/components/ui/badge';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { ChannelMark } from '@/components/ui/channel-mark';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { MonoLabel } from '@/components/ui/mono-label';
import { SupportedPlatforms } from '@/components/landing/SupportedPlatforms';
import { WaitlistForm } from '@/components/landing/WaitlistForm';
import { ChannelVote } from '@/components/landing/ChannelVote';
import { CONTACT_HREF, TELEGRAM_CHANNEL_URL } from '@/lib/constants/landing';
import { LandingInteractions } from '@/components/landing/LandingInteractions';
import { parseLandingEntryMode, pricingCta } from '@/lib/landing-entry';
import type { LandingEntryProps } from '@/lib/landing-entry';
import { legalDocHref } from '@/lib/legal/routes';

const MAX_REVIEW_STARS = 5;

// Static href anchors for nav. Labels are resolved via useTranslations
// inside each component.
const NAV_HREFS = ['#features', '#channels', '#pricing'] as const;

// ─── Page ─────────────────────────────────────────────────────────────

export const dynamic = 'force-dynamic';

export default function LandingPage() {
  const mode = parseLandingEntryMode(process.env.LANDING_ENTRY_MODE);
  return (
    <div className="min-h-screen bg-paper pb-[calc(5rem+env(safe-area-inset-bottom))] text-ink md:pb-0">
      <SiteNav mode={mode} />
      {/* tabIndex={-1}: makes <main> programmatically focusable so the
          SkipLink in app/layout.tsx can actually transfer keyboard focus
          here. focus-visible:outline-ink (keyboard-only) gives a brief
          visible cue that focus actually moved (WCAG 2.4.7). */}
      <main
        id="main-content"
        tabIndex={-1}
        className="focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
      >
        <Hero mode={mode} />
        <Belief />
        <Features />
        <HowItWorks />
        <Platforms />
        <Pricing mode={mode} />
        <Faq />
        <Quote />
        <Waitlist mode={mode} />
      </main>
      <SiteFooter />
      <LandingInteractions mode={mode} />
    </div>
  );
}

// ─── Nav ──────────────────────────────────────────────────────────────

function SiteNav({ mode }: LandingEntryProps) {
  const tNav = useTranslations('landing.nav');
  const navLabels: Record<(typeof NAV_HREFS)[number], string> = {
    '#features': tNav('features'),
    '#channels': tNav('channels'),
    '#pricing': tNav('pricing'),
  };
  return (
    <header className="sticky top-0 z-10 border-b border-line-soft bg-paper backdrop-blur">
      <div className="mx-auto flex w-full max-w-[1180px] flex-wrap items-center gap-x-2 gap-y-2 px-3 py-2 sm:px-12 lg:flex-nowrap">
        <Link href="/" className="flex items-center gap-2 text-[15px] font-semibold tracking-tight">
          <span className="inline-flex h-[26px] w-[26px] items-center justify-center rounded-md bg-ink text-[11px] font-semibold text-paper">
            {tNav('logoMark')}
          </span>
          {tNav('wordmark')}
        </Link>
        <a href="#pricing" className="ml-auto text-sm text-ink md:hidden">
          {tNav('pricing')}
        </a>
        <nav className="hidden items-center gap-6 text-sm text-ink-mid md:flex">
          {NAV_HREFS.map((href) => (
            <a key={href} href={href} className="transition-colors hover:text-ink">
              {navLabels[href]}
            </a>
          ))}
        </nav>
        <div className="ml-auto flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
          <div className="hidden sm:block">
            <LanguageSwitcher />
          </div>
          <Link
            href="/login"
            data-cta="nav-login"
            className="text-sm text-ink-mid transition-colors hover:text-ink"
          >
            {tNav('login')}
          </Link>
          {mode !== 'waitlist_only' && (
            <Link href="/register" data-cta="nav-register" className="text-sm text-ink">
              {tNav('register')}
            </Link>
          )}
          <Button asChild size="sm" variant="primary">
            <a
              href={mode === 'open' ? '/register' : '#waitlist'}
              data-cta={mode === 'open' ? 'nav-register' : 'nav-waitlist'}
            >
              {tNav(mode === 'open' ? 'start' : 'waitlist')}
            </a>
          </Button>
        </div>
      </div>
    </header>
  );
}

// ─── Hero ─────────────────────────────────────────────────────────────

function Hero({ mode }: LandingEntryProps) {
  const tHero = useTranslations('landing.hero');
  return (
    <section id="hero" className="border-b border-line-soft">
      <div className="mx-auto grid w-full max-w-[1180px] items-center gap-10 px-6 py-6 sm:px-12 md:py-28 lg:grid-cols-[1.1fr_0.9fr]">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <MonoLabel>{tHero('kicker')}</MonoLabel>
            <Badge tone="accent">{tHero('betaBadge')}</Badge>
          </div>
          <h1 className="mt-5 text-pretty font-display text-hero md:text-hero-lg">
            {tHero('headlineLine1')}
            <br />
            {tHero('headlineLine2Prefix')}
            <span className="font-display text-brand">{tHero('wordmark')}</span>
            {tHero('headlinePunctuation')}
          </h1>
          <p className="mt-4 max-w-[520px] text-[15px] leading-relaxed text-ink-mid sm:text-lg">
            {tHero('body')}
          </p>
          <p className="mt-3 text-sm text-ink">{tHero('audience')}</p>
          <div className="mt-5 flex flex-wrap gap-3">
            <Button asChild size="lg" variant="primary">
              <a
                href={mode === 'open' ? '/register' : '#waitlist'}
                data-cta={mode === 'open' ? 'hero-register' : 'hero-waitlist'}
              >
                {tHero(mode === 'open' ? 'start' : 'cta')}
                <ArrowRight aria-hidden />
              </a>
            </Button>
            <Button asChild size="lg" variant="secondary">
              <a href="#features">{tHero('secondary')}</a>
            </Button>
          </div>
          {mode !== 'waitlist_only' && (
            <p className="mt-4 text-sm text-ink">
              {tHero('accessLine')}{' '}
              <Link href="/login" data-cta="hero-login" className="underline">
                {tHero('login')}
              </Link>
              {' · '}
              <Link href="/register" data-cta="hero-register" className="underline">
                {tHero('register')}
              </Link>
            </p>
          )}
          <p className="mt-4 max-w-[460px] text-[13px] leading-relaxed text-ink-soft">
            {tHero('betaNote')}
          </p>
        </div>
        <HeroPreview />
      </div>
    </section>
  );
}

// Channel id + tone for each preview row stays in code (brand-icon id is
// not user-facing copy; tone is presentation, not content). Text is
// resolved from `landing.demo.inboxRows.<idx>` per locale.
const INBOX_ROW_META: Array<{
  channel: string;
  channelLabelKey: 'telegram' | 'vk' | 'yandex';
  tone: 'accent' | 'success' | 'neutral';
}> = [
  { channel: 'Telegram', channelLabelKey: 'telegram', tone: 'accent' },
  { channel: 'Yandex', channelLabelKey: 'yandex', tone: 'success' },
  { channel: 'VK', channelLabelKey: 'vk', tone: 'neutral' },
];

function HeroPreview() {
  const tPreview = useTranslations('landing.preview');
  const tDemo = useTranslations('landing.demo');
  const channelLabel: Record<'telegram' | 'vk' | 'yandex', string> = {
    telegram: 'Telegram',
    vk: 'VK',
    yandex: tDemo('yandexLabel'),
  };
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

        {INBOX_ROW_META.map((r, i) => (
          <div
            key={r.channel}
            className={`grid items-center gap-3 px-4 py-3 sm:grid-cols-[88px_1fr_auto] ${
              i < INBOX_ROW_META.length - 1 ? 'border-b border-line-soft' : ''
            }`}
          >
            <div className="flex items-center gap-2">
              <ChannelMark name={r.channel} size={20} />
              <span className="text-[12px] text-ink-soft">{channelLabel[r.channelLabelKey]}</span>
            </div>
            <div className="min-w-0">
              <div className="truncate text-[13px] font-medium">{tDemo(`inboxRows.${i}.name`)}</div>
              <div className="mt-0.5 truncate text-[13px] text-ink-mid">
                {tDemo(`inboxRows.${i}.message`)}
              </div>
            </div>
            <Badge tone={r.tone}>{tDemo(`inboxRows.${i}.label`)}</Badge>
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

// ─── Belief ───────────────────────────────────────────────────────────

function Belief() {
  const tBelief = useTranslations('landing.belief');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-20 sm:px-12">
        <MonoLabel>{tBelief('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[880px] text-pretty text-[28px] font-medium leading-[1.18] tracking-[-0.015em] sm:text-[34px]">
          {tBelief('headlinePrefix')}
          <span className="font-display text-brand">{tBelief('headlineHighlight')}</span>
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
  const samples: React.ReactNode[] = [
    <SampleInbox key="inbox" />,
    <SampleDraft key="draft" />,
    <SamplePosts key="posts" />,
    <SampleReviews key="reviews" />,
  ];

  return (
    <section id="features" className="scroll-mt-28 border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>{tF('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[32px] font-medium leading-tight tracking-[-0.015em] sm:text-[40px]">
          {tF('headline')}
        </h2>

        <div className="mt-12 grid grid-cols-1 gap-10 lg:grid-cols-2 lg:gap-x-16 lg:gap-y-14">
          {samples.map((sample, i) => (
            <article key={i} className="flex flex-col gap-6 border-t border-line pt-8">
              <div>
                <MonoLabel tone="ochre">{tF(`items.${i}.kicker`)}</MonoLabel>
                <h3 className="mt-2 text-[24px] font-medium leading-snug tracking-[-0.01em]">
                  {tF(`items.${i}.title`)}
                </h3>
                <p className="mt-3 max-w-[460px] text-[15px] leading-relaxed text-ink-mid">
                  {tF(`items.${i}.body`)}
                </p>
              </div>
              {sample}
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

const sampleBox = 'overflow-hidden rounded-lg border border-line bg-paper-raised';

// Channel id (brand icon hint) + label key stay in code; user-facing
// message + relative-time copy resolves through `landing.demo.sampleInbox`.
const SAMPLE_INBOX_META: Array<{ p: string; labelKey: 'telegram' | 'vk' | 'yandex' }> = [
  { p: 'Telegram', labelKey: 'telegram' },
  { p: 'VK', labelKey: 'vk' },
  { p: 'Yandex', labelKey: 'yandex' },
];

function SampleInbox() {
  const tDemo = useTranslations('landing.demo');
  const channelLabel: Record<'telegram' | 'vk' | 'yandex', string> = {
    telegram: 'Telegram',
    vk: 'VK',
    yandex: tDemo('yandexLabel'),
  };
  return (
    <div className={sampleBox} aria-hidden>
      {SAMPLE_INBOX_META.map((r, i) => (
        <div
          key={r.p}
          className={`grid items-center gap-3 px-4 py-3 sm:grid-cols-[96px_1fr_auto] ${
            i > 0 ? 'border-t border-line-soft' : ''
          }`}
        >
          <div className="flex items-center gap-2">
            <ChannelMark name={r.p} size={20} />
            <span className="text-[12px] text-ink-soft">{channelLabel[r.labelKey]}</span>
          </div>
          <div className="truncate text-[13px] text-ink-mid">
            {tDemo(`sampleInbox.${i}.message`)}
          </div>
          <MonoLabel>{tDemo(`sampleInbox.${i}.ago`)}</MonoLabel>
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
        <div className="rounded-md border border-brand-soft bg-brand-soft px-3.5 py-3 text-[13px] text-ink">
          {tS('answer')}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button asChild size="sm" variant="primary">
              <span>{tS('send')}</span>
            </Button>
            <Button asChild size="sm" variant="secondary">
              <span>{tS('fix')}</span>
            </Button>
            <Button asChild size="sm" variant="ghost">
              <span>{tS('tryAgain')}</span>
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
            <Button asChild size="sm" variant="primary">
              <span>{tS('schedule')}</span>
            </Button>
          </span>
        </div>
      </div>
    </div>
  );
}

// Star count, channel id, and replied flag stay in code (presentation /
// state, not copy). Author name and review text resolve via
// `landing.demo.sampleReviews.<idx>` per locale.
const SAMPLE_REVIEW_META: Array<{
  stars: number;
  plat: string;
  replied: boolean;
}> = [
  { stars: 5, plat: 'Yandex', replied: true },
  { stars: 3, plat: 'Yandex', replied: false },
];

function SampleReviews() {
  const tS = useTranslations('landing.sample');
  const tDemo = useTranslations('landing.demo');
  return (
    <div className={sampleBox} aria-hidden>
      {SAMPLE_REVIEW_META.map((r, i) => (
        <div key={i} className={`px-4 py-3 ${i > 0 ? 'border-t border-line-soft' : ''}`}>
          <div className="mb-1.5 flex items-center gap-2">
            <span className="text-[13px] tracking-[1px] text-ochre">
              {'★'.repeat(r.stars)}
              <span className="text-line">{'★'.repeat(MAX_REVIEW_STARS - r.stars)}</span>
            </span>
            <span className="text-[13px] font-medium">{tDemo(`sampleReviews.${i}.name`)}</span>
            <span className="ml-auto">
              <ChannelMark name={r.plat} size={20} />
            </span>
          </div>
          <div className="text-[13px] text-ink-mid">{tDemo(`sampleReviews.${i}.text`)}</div>
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

// Step numbers stay in code (decorative numeral, same in every locale);
// title + description resolve from `landing.howItWorks.steps.<idx>`.
const HOW_IT_WORKS_NUMERALS = ['01', '02', '03'] as const;

function HowItWorks() {
  const tHow = useTranslations('landing.howItWorks');
  return (
    <section className="border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>{tHow('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          {tHow('headline')}
        </h2>
        <div className="mt-14 grid grid-cols-1 gap-10 sm:grid-cols-3">
          {HOW_IT_WORKS_NUMERALS.map((n, i) => (
            <div key={n}>
              <span
                className="font-mono text-[15px] font-medium tracking-[0.04em] text-ochre-deep"
                aria-hidden
              >
                {n}
              </span>
              <h3 className="mt-3 text-[22px] font-medium tracking-[-0.005em]">
                {tHow(`steps.${i}.title`)}
              </h3>
              <p className="mt-2.5 text-[15px] leading-relaxed text-ink-mid">
                {tHow(`steps.${i}.body`)}
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
        <ChannelVote />
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
        <p className="mt-6 text-pretty text-[26px] font-normal leading-snug tracking-[-0.015em] text-ink sm:text-[32px]">
          {t('body')}
        </p>
        <p className="mt-7 text-[13px] text-ink-soft">{t('note')}</p>
      </div>
    </section>
  );
}

// ─── Pricing ──────────────────────────────────────────────────────────

// Tier meta stays in code (which tiers, feature counts, the highlighted one,
// which carries the beta-discount tag); all copy resolves from
// landing.pricing.tiers.<key> per locale.
const PRICING_TIERS: Array<{
  key: 'free' | 'pro' | 'enterprise';
  featureCount: number;
  highlight: boolean;
  hasBetaTag: boolean;
}> = [
  { key: 'free', featureCount: 3, highlight: false, hasBetaTag: false },
  { key: 'pro', featureCount: 4, highlight: true, hasBetaTag: true },
  { key: 'enterprise', featureCount: 3, highlight: false, hasBetaTag: false },
];

function Pricing({ mode }: LandingEntryProps) {
  const t = useTranslations('landing.pricing');
  return (
    <section id="pricing" className="scroll-mt-28 border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto w-full max-w-[1180px] px-6 py-24 sm:px-12">
        <MonoLabel>{t('kicker')}</MonoLabel>
        <h2 className="mt-3 max-w-[720px] text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[36px]">
          {t('headline')}
        </h2>
        <p className="mt-3 max-w-[640px] text-[16px] leading-relaxed text-ink-mid">{t('body')}</p>

        <div className="mt-12 grid grid-cols-1 gap-6 lg:grid-cols-3">
          {PRICING_TIERS.map((tier) => {
            const cta = pricingCta(tier.key, mode);
            return (
              <div
                key={tier.key}
                className={`flex min-w-0 flex-col rounded-xl border bg-paper-raised p-5 sm:p-6 ${
                  tier.highlight ? 'border-brand shadow-ov-3' : 'border-line'
                }`}
              >
                <MonoLabel tone={tier.highlight ? 'ochre' : undefined}>
                  {t(`tiers.${tier.key}.name`)}
                </MonoLabel>
                <div className="mt-3 flex flex-wrap items-baseline gap-1.5">
                  <span className="whitespace-nowrap text-[26px] font-medium tracking-[-0.02em] xl:text-[30px]">
                    {t(`tiers.${tier.key}.price`)}
                  </span>
                  <span className="text-[14px] text-ink-mid">{t(`tiers.${tier.key}.period`)}</span>
                </div>
                {tier.hasBetaTag && (
                  <p className="mt-3 rounded-md border border-brand-soft bg-brand-soft px-3 py-2 text-[13px] leading-snug text-ochre-ink">
                    {t(`tiers.${tier.key}.betaTag`)}
                    {mode === 'open' && (
                      <a
                        href="#waitlist"
                        data-cta="pricing-pro-waitlist"
                        className="mt-2 block underline"
                      >
                        {t('actions.waitlist')}
                      </a>
                    )}
                  </p>
                )}
                <ul className="mt-5 flex flex-1 flex-col gap-2.5 text-[14px] text-ink-mid">
                  {Array.from({ length: tier.featureCount }).map((_, i) => (
                    <li key={i} className="flex items-start gap-2">
                      <Check className="mt-0.5 size-4 shrink-0 text-ochre-deep" aria-hidden />
                      {t(`tiers.${tier.key}.features.${i}`)}
                    </li>
                  ))}
                </ul>
                <Button asChild variant={cta.variant} className="mt-6">
                  <a href={cta.href} data-cta={cta.tracking}>
                    {t(`actions.${cta.label}`)}
                  </a>
                </Button>
                {tier.key === 'free' && (
                  <p className="mt-3 text-sm text-ink-mid">{t('tiers.free.note')}</p>
                )}
              </div>
            );
          })}
        </div>

        <div className="mt-10 flex flex-col gap-4">
          <p className="max-w-[640px] text-[13px] leading-relaxed text-ink-soft">
            {t('compareNote')}
          </p>
        </div>
      </div>
    </section>
  );
}

// ─── FAQ ──────────────────────────────────────────────────────────────

// Question/answer copy resolves from landing.faq.items.<idx>; the count is
// fixed in code (same in every locale).
const FAQ_COUNT = 5;

function Faq() {
  const t = useTranslations('landing.faq');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[880px] px-6 py-24 sm:px-12">
        <MonoLabel>{t('kicker')}</MonoLabel>
        <h2 className="mt-3 text-[28px] font-medium leading-tight tracking-[-0.015em] sm:text-[34px]">
          {t('headline')}
        </h2>
        <dl className="mt-12 flex flex-col border-t border-line-soft">
          {Array.from({ length: FAQ_COUNT }).map((_, i) => (
            <div key={i} className="border-b border-line-soft py-6">
              <dt className="text-[17px] font-medium leading-snug tracking-[-0.005em]">
                {t(`items.${i}.q`)}
              </dt>
              <dd className="mt-2.5 max-w-[720px] text-[15px] leading-relaxed text-ink-mid">
                {t(`items.${i}.a`)}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}

// ─── Waitlist ─────────────────────────────────────────────────────────

function Waitlist({ mode }: LandingEntryProps) {
  const t = useTranslations('landing.waitlist');
  return (
    <section id="waitlist" className="scroll-mt-28 border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto grid w-full max-w-[1180px] items-start gap-12 px-6 py-24 sm:px-12 lg:grid-cols-[1fr_1fr] lg:gap-16">
        <div>
          <MonoLabel>{t('kicker')}</MonoLabel>
          <h2 className="mt-3 text-[30px] font-medium leading-tight tracking-[-0.015em] sm:text-[38px]">
            {t('headline')}
          </h2>
          <p className="mt-5 max-w-[480px] text-[16px] leading-relaxed text-ink-mid">{t('body')}</p>
        </div>
        <WaitlistForm mode={mode} />
      </div>
    </section>
  );
}

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
          <a
            href={TELEGRAM_CHANNEL_URL}
            target="_blank"
            rel="noreferrer"
            className="transition-colors hover:text-ink"
          >
            {tFooter('channel')}
          </a>
          <Link href={legalDocHref('privacy')} className="transition-colors hover:text-ink">
            {tFooter('privacy')}
          </Link>
          <a href={CONTACT_HREF} className="transition-colors hover:text-ink">
            {tFooter('contacts')}
          </a>
        </div>
      </div>
    </footer>
  );
}
