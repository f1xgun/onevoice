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

interface PlatformIconProps {
  platform: string;
  className?: string;
}

/** Decorative brand mark; the caller supplies the visible platform name. */
export function PlatformIcon({ platform, className }: PlatformIconProps) {
  const file = ICON_FILES[platform];
  if (!file) return <Globe aria-hidden className={cn('h-5 w-5 shrink-0', className)} />;
  return (
    <Image
      src={`/platforms/${file}.svg`}
      alt=""
      width={24}
      height={24}
      className={cn(
        'h-5 w-5 shrink-0 object-contain',
        platform !== '2gis' && 'dark:invert',
        className
      )}
    />
  );
}
