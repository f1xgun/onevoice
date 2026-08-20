// Telegram channel for the project, linked from the landing footer and the
// waitlist success screen. Overridable at build time so the placeholder can be
// swapped for the real channel without touching code.
export const TELEGRAM_CHANNEL_URL: string =
  process.env.NEXT_PUBLIC_TELEGRAM_CHANNEL_URL || 'https://t.me/onevoice';
