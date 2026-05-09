// Module-level translator for non-React contexts (Zod schemas, error
// maps, plain utility functions). React components should keep using
// `useTranslations` — this exists only for places where hooks aren't
// available, like a schema declaration that runs once at module load.
//
// Mirrors `DEFAULT_LOCALE` from `lib/i18n/request.ts` but pulls
// `next-intl`'s client-safe entry point so this module ships in client
// bundles too. Keep the literal in sync if/when the request config
// changes.
//
// `getTranslator` is overloaded to accept any of next-intl's typed
// `NamespaceKeys` plus the no-namespace form. This relays next-intl's
// own key-validation through to call-sites — `getTranslator('validation')`
// is checked against ru.json keys exactly as `useTranslations` would be.

import { createTranslator } from 'next-intl';
import type { NamespaceKeys, NestedKeyOf } from 'next-intl';
import messages from '@/messages/ru.json';

type Messages = typeof messages;
type Namespaces = NamespaceKeys<Messages, NestedKeyOf<Messages>>;

const LOCALE = 'ru';

export function getTranslator<N extends Namespaces>(namespace: N) {
  return createTranslator({ locale: LOCALE, messages, namespace });
}
