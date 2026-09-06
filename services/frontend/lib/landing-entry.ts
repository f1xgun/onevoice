import { CONTACT_HREF } from '@/lib/constants/landing';

export type LandingEntryMode = 'waitlist_only' | 'hybrid' | 'open';
export interface LandingEntryProps {
  mode: LandingEntryMode;
}
export interface LandingCta {
  href: '/register' | '#waitlist' | typeof CONTACT_HREF;
  label: 'start' | 'waitlist' | 'betaAccess' | 'contact';
  variant: 'primary' | 'secondary';
  tracking?: string;
}

export function parseLandingEntryMode(value: string | undefined): LandingEntryMode {
  return value === 'waitlist_only' || value === 'open' ? value : 'hybrid';
}

export function pricingCta(
  tier: 'free' | 'pro' | 'enterprise',
  mode: LandingEntryMode
): LandingCta {
  if (tier === 'enterprise') return { href: CONTACT_HREF, label: 'contact', variant: 'secondary' };
  if (tier === 'free') {
    return mode === 'waitlist_only'
      ? { href: '#waitlist', label: 'betaAccess', variant: 'secondary' }
      : {
          href: '/register',
          label: 'start',
          variant: 'secondary',
          tracking: 'pricing-free-register',
        };
  }
  return mode === 'open'
    ? { href: '/register', label: 'start', variant: 'primary', tracking: 'pricing-pro-register' }
    : {
        href: '#waitlist',
        label: 'waitlist',
        variant: 'primary',
        tracking: 'pricing-pro-waitlist',
      };
}
