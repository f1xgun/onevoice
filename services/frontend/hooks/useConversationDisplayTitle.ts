'use client';

import { useLocale, useTranslations } from 'next-intl';
import type { TitleStatus } from '@/lib/conversations';

interface ConversationTitle {
  title?: string;
  titleStatus?: TitleStatus;
  createdAt?: string;
}

const LEAP_YEAR = 2000;

const russianMonths =
  'января|февраля|марта|апреля|мая|июня|июля|августа|сентября|октября|ноября|декабря';
const englishMonths =
  'January|February|March|April|May|June|July|August|September|October|November|December';
const russianFallback = new RegExp(
  `^(?:Без названия|Untitled chat) ([1-9]|[12][0-9]|3[01]) (${russianMonths})$`
);
const englishFallback = new RegExp(`^Untitled chat (${englishMonths}) ([1-9]|[12][0-9]|3[01])$`);

function legacyFallbackDate(title: string): Date | undefined {
  const ru = russianFallback.exec(title);
  const en = englishFallback.exec(title);
  if (ru?.[0] !== title && en?.[0] !== title) return undefined;
  const day = Number(ru ? ru[1] : en![2]);
  const month = (ru ? russianMonths : englishMonths).split('|').indexOf(ru ? ru[2] : en![1]);
  const date = new Date(Date.UTC(LEAP_YEAR, month, day));
  return date.getUTCMonth() === month ? date : undefined;
}

export function useConversationDisplayTitle() {
  const locale = useLocale();
  const t = useTranslations('chat');
  return (conversation: ConversationTitle): string => {
    const { title = '', titleStatus, createdAt } = conversation;
    if (titleStatus === 'manual') return title;
    const legacyDate = legacyFallbackDate(title);
    const fallbackDate =
      legacyDate ??
      (title === '' && titleStatus === 'auto' && createdAt ? new Date(createdAt) : undefined);
    if (fallbackDate && !Number.isNaN(fallbackDate.getTime())) {
      const date = new Intl.DateTimeFormat(locale === 'en' ? 'en-GB' : 'ru-RU', {
        day: 'numeric',
        month: 'long',
        timeZone: 'UTC',
      }).format(fallbackDate);
      return t('fallbackDate', { date });
    }
    if (titleStatus === 'auto_pending' || title === '') return t('newConversation');
    return title;
  };
}
