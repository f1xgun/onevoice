import Link from 'next/link';
import { ArrowRight, Check } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { ThemeSwitcher } from '@/components/design-system/ThemeSwitcher';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { WorkExample } from '@/components/landing/WorkExample';
import { SupportedPlatforms } from '@/components/landing/SupportedPlatforms';
import { WaitlistForm } from '@/components/landing/WaitlistForm';
import { ChannelVote } from '@/components/landing/ChannelVote';
import { CONTACT_HREF, TELEGRAM_CHANNEL_URL } from '@/lib/constants/landing';
import { LandingInteractions } from '@/components/landing/LandingInteractions';
import { parseLandingEntryMode, pricingCta } from '@/lib/landing-entry';
import type { LandingEntryProps } from '@/lib/landing-entry';
import { legalDocHref } from '@/lib/legal/routes';

const NAV_HREFS = ['#features', '#channels', '#pricing'] as const;

export const dynamic = 'force-dynamic';

export default function LandingPage() {
  const mode = parseLandingEntryMode(process.env.LANDING_ENTRY_MODE);
  return (
    <div
      data-ov-motion
      className="min-h-screen bg-paper pb-[calc(5rem+env(safe-area-inset-bottom))] text-ink [overflow-wrap:anywhere] md:pb-0"
    >
      <SiteNav mode={mode} />
      <main
        id="main-content"
        tabIndex={-1}
        className="focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink"
      >
        <Hero mode={mode} />
        <WorkExample />
        <Features />
        <Platforms />
        <Pricing mode={mode} />
        <Faq />
        <Waitlist mode={mode} />
      </main>
      <SiteFooter />
      <LandingInteractions mode={mode} />
    </div>
  );
}

function SiteNav({ mode }: LandingEntryProps) {
  const tNav = useTranslations('landing.nav');
  const navLabels: Record<(typeof NAV_HREFS)[number], string> = {
    '#features': tNav('features'),
    '#channels': tNav('channels'),
    '#pricing': tNav('pricing'),
  };
  return (
    <header className="relative z-10 border-b border-line-soft bg-paper backdrop-blur md:sticky md:top-0">
      <div className="mx-auto flex w-full max-w-[1120px] flex-wrap items-center gap-x-2 gap-y-2 px-3 py-2 sm:px-10 lg:flex-nowrap">
        <Link href="/" className="flex items-center gap-2 text-[15px] font-semibold tracking-tight">
          <span className="inline-flex h-[26px] w-[26px] items-center justify-center rounded-md bg-ink text-[11px] font-semibold text-paper">
            {tNav('logoMark')}
          </span>
          {tNav('wordmark')}
        </Link>
        <a
          href="#pricing"
          className="ml-auto inline-flex min-h-11 items-center text-meta text-ink underline md:hidden"
        >
          {tNav('pricing')}
        </a>
        <div className="flex items-center gap-1">
          <ThemeSwitcher />
          <LanguageSwitcher className="min-h-11 min-w-11" />
        </div>
        <nav className="hidden items-center gap-6 text-sm text-ink-mid md:flex">
          {NAV_HREFS.map((href) => (
            <a key={href} href={href} className="underline hover:text-brand">
              {navLabels[href]}
            </a>
          ))}
        </nav>
        <div className="ml-auto flex w-full flex-wrap items-center justify-between gap-2 sm:w-auto sm:justify-end sm:gap-3">
          <Link
            href="/login"
            data-cta="nav-login"
            className="inline-flex min-h-11 items-center text-meta text-ink underline hover:text-brand"
          >
            {tNav('login')}
          </Link>
          {mode !== 'waitlist_only' && (
            <Link
              href="/register"
              data-cta="nav-register"
              className="inline-flex min-h-11 items-center text-meta text-ink underline"
            >
              {tNav('register')}
            </Link>
          )}
          <a
            href={mode === 'open' ? '/register' : '#waitlist'}
            data-cta={mode === 'open' ? 'nav-register' : 'nav-waitlist'}
            className="inline-flex min-h-11 items-center text-meta font-medium text-brand underline"
          >
            {tNav(mode === 'open' ? 'start' : 'waitlist')}
          </a>
        </div>
      </div>
    </header>
  );
}

function Hero({ mode }: LandingEntryProps) {
  const tHero = useTranslations('landing.hero');
  return (
    <section id="hero" className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1120px] px-5 py-6 sm:px-10 md:py-12">
        <div>
          <p className="text-meta text-ink-soft">{tHero('betaBadge')}</p>
          <h1 className="mt-3 max-w-[24ch] text-pretty font-display text-hero md:text-hero-lg">
            {tHero('headline')}
          </h1>
          <p className="mt-3 max-w-[66ch] text-reading">{tHero('body')}</p>
          <p className="mt-3 text-sm text-ink">{tHero('audience')}</p>
          <div className="mt-4 flex flex-wrap gap-3">
            <Button asChild size="lg" variant="primary">
              <a
                href={mode === 'open' ? '/register' : '#waitlist'}
                data-cta={mode === 'open' ? 'hero-register' : 'hero-waitlist'}
              >
                {tHero(mode === 'open' ? 'start' : 'cta')}
                <ArrowRight aria-hidden />
              </a>
            </Button>
          </div>
          {mode !== 'waitlist_only' && (
            <p className="mt-3 text-meta text-ink">
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
          <p className="mt-3 max-w-[66ch] text-meta text-ink-soft">{tHero('betaNote')}</p>
        </div>
      </div>
    </section>
  );
}

function Features() {
  const t = useTranslations('landing.scenarios');
  return (
    <section id="features" className="scroll-mt-28 border-b border-line">
      <div className="mx-auto w-full max-w-[1120px] px-5 py-12 sm:px-10 md:py-20">
        <h2 className="text-section md:text-section-lg">{t('title')}</h2>
        <div className="mt-8 grid gap-8 md:grid-cols-2">
          {(['posts', 'replies'] as const).map((scenario) => (
            <article key={scenario} className="border-t border-line pt-4">
              <p className="text-meta text-ink-soft">{t(`${scenario}.platforms`)}</p>
              <h3 className="mt-3 text-document-title">{t(`${scenario}.title`)}</h3>
              <p className="mt-3 max-w-[66ch] text-reading">{t(`${scenario}.body`)}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function Platforms() {
  const tCh = useTranslations('landing.channels');
  return (
    <section id="channels" className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[1120px] px-5 py-12 sm:px-10 md:py-20">
        <p className="text-meta text-ink-soft">{tCh('kicker')}</p>
        <h2 className="mt-3 max-w-[720px] text-section md:text-section-lg">{tCh('headline')}</h2>
        <p className="mt-3 max-w-[640px] text-reading text-ink">{tCh('body')}</p>
        <SupportedPlatforms />
        <ChannelVote />
      </div>
    </section>
  );
}

const PRICING_TIERS: Array<{
  key: 'free' | 'pro' | 'enterprise';
  featureCount: number;
  hasBetaTag: boolean;
}> = [
  { key: 'free', featureCount: 3, hasBetaTag: false },
  { key: 'pro', featureCount: 4, hasBetaTag: true },
  { key: 'enterprise', featureCount: 3, hasBetaTag: false },
];

function Pricing({ mode }: LandingEntryProps) {
  const t = useTranslations('landing.pricing');
  return (
    <section id="pricing" className="scroll-mt-28 border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto w-full max-w-[1120px] px-5 py-12 sm:px-10 md:py-20">
        <p className="text-meta text-ink-soft">{t('kicker')}</p>
        <h2 className="mt-3 max-w-[720px] text-section md:text-section-lg">{t('headline')}</h2>
        <p className="mt-3 max-w-[640px] text-reading text-ink">{t('body')}</p>

        <div className="mt-12 grid grid-cols-1 gap-6 lg:grid-cols-3">
          {PRICING_TIERS.map((tier) => {
            const cta = pricingCta(tier.key, mode);
            return (
              <div
                key={tier.key}
                className="flex min-w-0 flex-col rounded-lg border border-line bg-paper-raised p-4 sm:p-6"
              >
                <p className="text-meta text-ink-soft">{t(`tiers.${tier.key}.name`)}</p>
                <div className="mt-3 flex flex-wrap items-baseline gap-1.5">
                  <span className="max-w-full whitespace-normal text-price">
                    {t(`tiers.${tier.key}.price`)}
                  </span>
                  <span className="text-meta text-ink">{t(`tiers.${tier.key}.period`)}</span>
                </div>
                {tier.hasBetaTag && (
                  <p className="mt-3 rounded-md border border-brand-soft bg-brand-soft px-3 py-2 text-meta text-ink">
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
                <ul className="mt-5 flex flex-1 flex-col gap-2.5 text-meta text-ink">
                  {Array.from({ length: tier.featureCount }).map((_, i) => (
                    <li key={i} className="flex items-start gap-2">
                      <Check className="mt-0.5 size-4 shrink-0 text-brand" aria-hidden />
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
          <p className="max-w-[640px] text-meta text-ink-soft">{t('compareNote')}</p>
        </div>
      </div>
    </section>
  );
}

const FAQ_COUNT = 5;

function Faq() {
  const t = useTranslations('landing.faq');
  return (
    <section className="border-b border-line-soft">
      <div className="mx-auto w-full max-w-[880px] px-5 py-12 sm:px-10 md:py-20">
        <p className="text-meta text-ink-soft">{t('kicker')}</p>
        <h2 className="mt-3 text-section md:text-section-lg">{t('headline')}</h2>
        <dl className="mt-12 flex flex-col border-t border-line-soft">
          {Array.from({ length: FAQ_COUNT }).map((_, i) => (
            <div key={i} className="border-b border-line-soft py-6">
              <dt className="text-action">{t(`items.${i}.q`)}</dt>
              <dd className="mt-2.5 max-w-[720px] text-reading text-ink">{t(`items.${i}.a`)}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}

function Waitlist({ mode }: LandingEntryProps) {
  const t = useTranslations('landing.waitlist');
  return (
    <section id="waitlist" className="scroll-mt-28 border-b border-line-soft bg-paper-sunken">
      <div className="mx-auto grid w-full min-w-0 max-w-[1120px] grid-cols-1 items-start gap-8 px-5 py-12 sm:px-10 md:py-20 lg:grid-cols-[1fr_1fr] lg:gap-16">
        <div>
          <p className="text-meta text-ink-soft">{t('kicker')}</p>
          <h2 className="mt-3 text-section md:text-section-lg">{t('headline')}</h2>
          <p className="mt-5 max-w-[480px] text-reading text-ink">{t('body')}</p>
        </div>
        <WaitlistForm mode={mode} />
      </div>
    </section>
  );
}

function SiteFooter() {
  const tFooter = useTranslations('landing.footer');
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex w-full max-w-[1120px] flex-wrap items-center gap-6 px-5 py-10 text-meta text-ink-soft sm:px-10">
        <span className="flex items-center gap-2 font-semibold text-ink">
          <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-ink text-[10px] text-paper">
            {tFooter('logoMark')}
          </span>
          {tFooter('wordmark')}
        </span>
        <p className="text-meta text-ink-soft">
          {tFooter('rights', { year: new Date().getFullYear() })}
        </p>
        <div className="ml-auto flex flex-wrap items-center gap-5">
          <a
            href={TELEGRAM_CHANNEL_URL}
            target="_blank"
            rel="noreferrer"
            className="underline hover:text-brand"
          >
            {tFooter('channel')}
          </a>
          <Link href={legalDocHref('privacy')} className="underline hover:text-brand">
            {tFooter('privacy')}
          </Link>
          <a href={CONTACT_HREF} className="underline hover:text-brand">
            {tFooter('contacts')}
          </a>
        </div>
      </div>
    </footer>
  );
}
