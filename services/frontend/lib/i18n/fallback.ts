import { IntlErrorCode } from 'next-intl';
import type { IntlError } from 'next-intl';

// Shared next-intl error + fallback handlers, used by both the server request
// config (lib/i18n/request.ts) and the client provider so server- and
// client-rendered text degrade identically when a key is absent.

export function onIntlError(error: IntlError): void {
  // A missing key is non-fatal: getMessageFallback renders a readable stand-in.
  // The ru/en parity drift guard catches missing keys in CI, so swallowing the
  // runtime error here only prevents console noise / render crashes in prod.
  if (error.code === IntlErrorCode.MISSING_MESSAGE) {
    return;
  }
  // eslint-disable-next-line no-console
  console.error(error);
}

export function intlMessageFallback({
  key,
  namespace,
}: {
  key: string;
  namespace?: string;
}): string {
  const full = namespace ? `${namespace}.${key}` : key;
  const last = full.split('.').pop() ?? full;
  return last.replace(/[_-]/g, ' ');
}
