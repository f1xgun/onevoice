vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import LandingPage from '@/app/page';
import { WaitlistForm } from '../WaitlistForm';
import { LandingInteractions } from '../LandingInteractions';
import { parseLandingEntryMode } from '@/lib/landing-entry';
import { joinWaitlist } from '@/lib/api/waitlist';
import ru from '@/messages/ru.json';
import en from '@/messages/en.json';

vi.mock('@/components/ui/LanguageSwitcher', () => ({ LanguageSwitcher: () => null }));
vi.mock('@/components/landing/ChannelVote', () => ({ ChannelVote: () => null }));
vi.mock('@/components/landing/SupportedPlatforms', () => ({ SupportedPlatforms: () => null }));
vi.mock('@/lib/api/waitlist', () => ({ joinWaitlist: vi.fn().mockResolvedValue({}) }));
vi.stubGlobal(
  'IntersectionObserver',
  class {
    observe() {}
    disconnect() {}
  }
);
afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

const modes = ['waitlist_only', 'hybrid', 'open'] as const;
describe('landing entry', () => {
  it.each([undefined, '', 'invalid', 'hybrid'])('defaults %s to hybrid', (value) => {
    expect(parseLandingEntryMode(value)).toBe('hybrid');
  });
  it.each(modes.flatMap((mode) => (['ru', 'en'] as const).map((locale) => ({ mode, locale }))))(
    'renders $mode in $locale from runtime environment',
    ({ mode, locale }) => {
      (globalThis as unknown as { __setTestLocale: (locale: 'ru' | 'en') => void }).__setTestLocale(
        locale
      );
      const copy = locale === 'ru' ? ru : en;
      vi.stubEnv('LANDING_ENTRY_MODE', mode);
      const { container } = render(<LandingPage />);
      const registrations = container.querySelectorAll('a[href="/register"]');
      expect(registrations.length > 0).toBe(mode !== 'waitlist_only');
      expect(container.querySelector('#hero a[data-cta]')).toHaveAttribute(
        'href',
        mode === 'open' ? '/register' : '#waitlist'
      );
      expect(
        container.querySelectorAll('#pricing a[href="mailto:hello@onevoice.app"]')
      ).toHaveLength(1);
      expect(
        container.querySelectorAll(
          '[aria-hidden="true"] button, [aria-hidden="true"] a, [aria-hidden="true"] [tabindex="0"]'
        )
      ).toHaveLength(0);
      const example = screen.getByRole('region', { name: copy.landing.workExample.title });
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
        copy.landing.hero.headline
      );
      expect(within(example).getByText(copy.landing.workExample.draft)).toBeVisible();
      expect(within(example).getByText(copy.landing.workExample.demo)).toBeVisible();
      expect(within(example).getByText(copy.landing.workExample.decision)).toBeVisible();
      expect(example.closest('[aria-hidden="true"]')).toBeNull();
      expect(example.querySelectorAll('button, a, [role="button"]')).toHaveLength(0);
      expect(example.querySelector('del')).toHaveTextContent(copy.landing.workExample.before);
      expect(example.querySelector('ins')).toHaveTextContent(copy.landing.workExample.after);
      expect(container.querySelectorAll('#features article')).toHaveLength(2);
      for (const tier of Object.values(copy.landing.pricing.tiers)) {
        expect(
          within(container.querySelector<HTMLElement>('#pricing')!).getByText(tier.name)
        ).toBeVisible();
        for (const feature of Object.values(tier.features))
          expect(screen.getByText(feature)).toBeVisible();
      }
      expect(screen.getByText(copy.landing.hero.betaBadge)).toBeVisible();
      expect(screen.getByText(copy.landing.hero.audience)).toBeVisible();
      expect(screen.getByText(copy.landing.hero.betaNote)).toBeVisible();
      expect(container.querySelector('header a[data-cta="nav-login"]')).toHaveAttribute(
        'href',
        '/login'
      );
      expect(container.querySelector('#pricing a[data-cta="pricing-free-register"]') !== null).toBe(
        mode !== 'waitlist_only'
      );
      expect(container.querySelectorAll('#pricing a[href="#waitlist"]')).toHaveLength(
        mode === 'waitlist_only' ? 2 : 1
      );
      expect(container.querySelectorAll('header a[href="#pricing"]').length).toBeGreaterThan(0);
      expect(screen.getByText(copy.landing.pricing.tiers.free.note)).toBeVisible();
      expect(
        screen.getByText(copy.landing.pricing.tiers.pro.price, { normalizer: (value) => value })
      ).toBeVisible();
    }
  );
  it('keeps locale key sets and nonbreaking prices aligned', () => {
    function keys(value: object, prefix = ''): string[] {
      return Object.entries(value).flatMap(([key, child]) =>
        typeof child === 'object' ? keys(child, `${prefix}${key}.`) : [`${prefix}${key}`]
      );
    }
    expect(keys(ru)).toEqual(keys(en));
    for (const bundle of [ru, en])
      expect(bundle.landing.pricing.tiers.pro.price).toBe('3\u00a0990–4\u00a0990\u00a0₽');
  });
  it.each(modes)('offers registration after waitlist success in %s', async (mode) => {
    render(<WaitlistForm mode={mode} />);
    fireEvent.change(screen.getByLabelText(ru.landing.waitlist.emailLabel), {
      target: { value: 'person@example.com' },
    });
    fireEvent.click(screen.getByRole('checkbox'));
    const submit = screen.getByRole('button', { name: ru.landing.waitlist.submit });
    await waitFor(() => expect(submit).toBeEnabled());
    fireEvent.click(submit);
    await screen.findByText(ru.landing.waitlist.success.title);
    expect(joinWaitlist).toHaveBeenCalled();
    expect(document.querySelector('a[data-cta="waitlist-success-register"]') !== null).toBe(
      mode !== 'waitlist_only'
    );
    expect(screen.getByRole('link', { name: ru.landing.waitlist.success.cta })).toHaveAttribute(
      'href',
      expect.stringContaining('https://t.me/')
    );
  });
});

describe('public CTA tracking', () => {
  it.each(['beacon', 'offline', '429', 'timeout', 'throw'])(
    'does not block or duplicate navigation with %s',
    async (outcome) => {
      const beacon = vi.fn().mockReturnValue(outcome === 'beacon');
      Object.defineProperty(navigator, 'sendBeacon', { configurable: true, value: beacon });
      const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(() => {
        if (outcome === 'throw') throw new Error('offline');
        if (outcome === 'offline') return Promise.reject(new Error('offline'));
        if (outcome === 'timeout') return new Promise(() => {});
        return Promise.resolve(new Response(null, { status: 429 }));
      });
      const navigation = vi.fn();
      render(
        <>
          <LandingInteractions mode="hybrid" />
          <a
            href="#destination"
            data-cta="hero-register"
            onClick={(event) => navigation(event.defaultPrevented)}
          >
            Continue
          </a>
          <div id="destination" />
        </>
      );
      const user = userEvent.setup();
      await user.click(screen.getByRole('link'));
      expect(navigation).toHaveBeenLastCalledWith(false);
      expect(beacon).toHaveBeenCalledTimes(1);
      expect(fetchMock).toHaveBeenCalledTimes(outcome === 'beacon' ? 0 : 1);
      await user.keyboard('{Enter}');
      expect(navigation).toHaveBeenCalledTimes(2);
      expect(beacon).toHaveBeenCalledTimes(2);
      if (outcome !== 'beacon')
        expect(fetchMock).toHaveBeenCalledWith(
          '/api/v1/landing-events',
          expect.objectContaining({
            credentials: 'omit',
            keepalive: true,
            body: JSON.stringify({ cta: 'hero-register', path: '/' }),
          })
        );
    }
  );
  it('hides mobile bar around the form and focused inputs', () => {
    const { container } = render(
      <>
        <section id="hero" />
        <section id="waitlist" />
        <input aria-label="Email" />
        <LandingInteractions mode="open" />
      </>
    );
    vi.spyOn(document.getElementById('hero')!, 'getBoundingClientRect').mockReturnValue({
      bottom: -1,
    } as DOMRect);
    const rect = vi
      .spyOn(document.getElementById('waitlist')!, 'getBoundingClientRect')
      .mockReturnValue({ top: 2000, bottom: 3000 } as DOMRect);
    fireEvent.scroll(window);
    expect(container.querySelector('[data-cta="mobile-register"]')).not.toBeNull();
    rect.mockReturnValue({ top: 100, bottom: 1000 } as DOMRect);
    fireEvent.scroll(window);
    expect(container.querySelector('[data-cta="mobile-register"]')).toBeNull();
    rect.mockReturnValue({ top: 2000, bottom: 3000 } as DOMRect);
    act(() => screen.getByRole('textbox').focus());
    fireEvent.scroll(window);
    expect(container.querySelector('[data-cta="mobile-register"]')).toBeNull();
  });
});

describe('waitlist choices and recovery', () => {
  it('toggles choices through their labels and submits the selected payload', async () => {
    const user = userEvent.setup();
    render(<WaitlistForm mode="hybrid" />);
    await user.type(screen.getByLabelText(ru.landing.waitlist.emailLabel), 'owner@example.com');
    for (const option of ['reviews', 'posts', 'card'] as const) {
      await user.click(screen.getByText(ru.landing.waitlist.painOptions[option]));
      expect(
        screen.getByRole('radio', { name: ru.landing.waitlist.painOptions[option] })
      ).toBeChecked();
    }
    await user.click(screen.getByText(ru.landing.waitlist.consentLabel, { exact: false }));
    expect(screen.getByRole('checkbox')).toBeChecked();
    await user.click(screen.getByRole('button', { name: ru.landing.waitlist.submit }));
    await screen.findByText(ru.landing.waitlist.success.title);
    expect(joinWaitlist).toHaveBeenLastCalledWith({
      email: 'owner@example.com',
      consent: true,
      pain: 'card',
    });
  });
  it('associates validation with email and keeps data after server failure', async () => {
    const user = userEvent.setup();
    vi.mocked(joinWaitlist).mockRejectedValueOnce({ response: { status: 429 } });
    render(<WaitlistForm mode="hybrid" />);
    const email = screen.getByLabelText(ru.landing.waitlist.emailLabel);
    await user.type(email, 'invalid');
    expect(email).toHaveAttribute('aria-invalid', 'true');
    expect(email).toHaveAccessibleDescription();
    await user.clear(email);
    await user.type(email, 'owner@example.com');
    await user.click(screen.getByRole('checkbox'));
    await user.click(screen.getByRole('button', { name: ru.landing.waitlist.submit }));
    expect(await screen.findByRole('alert')).toHaveTextContent(ru.landing.waitlist.errorRateLimit);
    expect(email).toHaveValue('owner@example.com');
    expect(screen.getByRole('checkbox')).toBeChecked();
    expect(screen.getByRole('button', { name: ru.landing.waitlist.submit })).toBeEnabled();
  });
});

describe('mobile visual viewport', () => {
  it('hides for the keyboard and low focus, restores after resize, and removes listeners', () => {
    const viewport = new EventTarget();
    Object.assign(viewport, { height: window.innerHeight, offsetTop: 0 });
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: viewport });
    const remove = vi.spyOn(viewport, 'removeEventListener');
    const { container, unmount } = render(
      <>
        <section id="hero" />
        <section id="waitlist" />
        <a href="#pricing">Focus</a>
        <LandingInteractions mode="hybrid" />
      </>
    );
    vi.spyOn(document.getElementById('hero')!, 'getBoundingClientRect').mockReturnValue({
      bottom: -1,
    } as DOMRect);
    vi.spyOn(document.getElementById('waitlist')!, 'getBoundingClientRect').mockReturnValue({
      top: 2000,
      bottom: 3000,
    } as DOMRect);
    fireEvent.scroll(window);
    expect(container.querySelector('[data-landing-bar]')).not.toBeNull();
    Object.assign(viewport, { height: window.innerHeight - 300 });
    act(() => viewport.dispatchEvent(new Event('resize')));
    expect(container.querySelector('[data-landing-bar]')).toBeNull();
    Object.assign(viewport, { height: window.innerHeight });
    act(() => viewport.dispatchEvent(new Event('resize')));
    expect(container.querySelector('[data-landing-bar]')).not.toBeNull();
    vi.spyOn(
      container.querySelector('[data-landing-bar]')!,
      'getBoundingClientRect'
    ).mockReturnValue({ height: 160 } as DOMRect);
    const link = screen.getByRole('link', { name: 'Focus' });
    vi.spyOn(link, 'getBoundingClientRect').mockReturnValue({
      bottom: window.innerHeight - 120,
    } as DOMRect);
    act(() => link.focus());
    expect(container.querySelector('[data-landing-bar]')).toBeNull();
    unmount();
    expect(remove).toHaveBeenCalledWith('resize', expect.any(Function));
    expect(remove).toHaveBeenCalledWith('scroll', expect.any(Function));
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: undefined });
  });
});
