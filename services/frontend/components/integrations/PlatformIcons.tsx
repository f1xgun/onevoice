import Image from 'next/image';
import { Globe } from 'lucide-react';

import { cn } from '@/lib/utils';

const ICON_FILES: Record<string, string> = {
  telegram: 'telegram',
  vk: 'vk',
  yandex_business: 'yandex-business',
  google_business: 'google',
  google: 'google',
  '2gis': '2gis',
  avito: 'avito',
  whatsapp: 'whatsapp',
  instagram: 'instagram',
  ok: 'odnoklassniki',
};

const BRAND_FILES: Record<string, string> = {
  telegram: 'telegram-brand',
  vk: 'vk-brand',
  yandex_business: 'yandex-business-brand',
};

interface PlatformIconProps {
  platform: string;
  className?: string;
  variant?: 'brand' | 'monochrome';
}

/** Decorative brand mark; the caller supplies the visible platform name. */
export function PlatformIcon({ platform, className, variant = 'brand' }: PlatformIconProps) {
  const brandFile = variant === 'brand' ? BRAND_FILES[platform] : undefined;
  const file = brandFile ?? ICON_FILES[platform];
  if (!file) return <Globe aria-hidden className={cn('h-8 w-8 shrink-0', className)} />;
  return (
    <Image
      src={`/platforms/${file}.svg`}
      alt=""
      width={24}
      height={24}
      className={cn(
        'h-8 w-8 shrink-0 object-contain',
        !brandFile && platform !== '2gis' && 'dark:invert',
        className
      )}
    />
  );
}
