'use client';

import { NextIntlClientProvider } from 'next-intl';
import type { AbstractIntlMessages } from 'next-intl';
import type { ReactNode } from 'react';
import { DEFAULT_TIME_ZONE } from '@/lib/i18n/timeZone';
import { intlMessageFallback, onIntlError } from '@/lib/i18n/fallback';

interface IntlClientProviderProps {
  locale: string;
  messages: AbstractIntlMessages;
  children: ReactNode;
}

// Client wrapper around NextIntlClientProvider. onError/getMessageFallback are
// functions and cannot be passed across the server→client boundary from the
// root layout, so they are bound here in a Client Component.
export function IntlClientProvider({ locale, messages, children }: IntlClientProviderProps) {
  return (
    <NextIntlClientProvider
      locale={locale}
      timeZone={DEFAULT_TIME_ZONE}
      messages={messages}
      onError={onIntlError}
      getMessageFallback={intlMessageFallback}
    >
      {children}
    </NextIntlClientProvider>
  );
}
