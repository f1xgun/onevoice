import type { JSX } from 'react';

function TelegramIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M20.665 3.717l-17.73 6.837c-1.21.486-1.203 1.161-.222 1.462l4.552 1.42 10.532-6.645c.498-.303.953-.14.579.192l-8.533 7.701h-.002l.002.001-.314 4.692c.46 0 .663-.211.921-.46l2.211-2.15 4.599 3.397c.848.467 1.457.227 1.668-.785l3.019-14.228c.309-1.239-.473-1.8-1.282-1.434z" />
    </svg>
  );
}

function VKIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M21.547 7h-2.318c-.418 0-.61.219-.805.547 0 0-1.647 2.89-2.758 3.845-.27.259-.505.342-.676.342-.096 0-.233-.083-.233-.407V7.547C14.757 7.193 14.66 7 14.1 7H11.1c-.418 0-.67.31-.67.602 0 .631.945.777.945.777v3.274c0 .718-.325.777-.325.777-.596 0-2.044-2.19-2.905-3.695C7.903 8.263 7.614 7 7.194 7H4.6c-.471 0-.565.219-.565.547 0 .686 1.452 4.213 3.452 6.717C9.303 16.692 11.6 18 13.4 18c1.076 0 1.21-.242 1.21-.66v-2.12c0-.473.1-.567.43-.567.244 0 .663.122 1.64 1.06C17.82 16.863 17.967 18 18.838 18h2.16c.472 0 .707-.242.572-.72-.149-.473-1.746-2.141-1.832-2.249-.244-.319-.176-.462 0-.746 0 0 2.458-3.463 2.716-4.638.128-.391 0-.647-.547-.647z" />
    </svg>
  );
}

function YandexIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="80 60 360 400" fill="currentColor">
      <path d="M283.718 362.624v53.781h-55.062v-90.687L124.75 99.75h57.456l80.95 176.78c15.606 33.782 20.562 45.52 20.562 86.094zM387.249 99.75l-67.55 153.093h-55.981L331.262 99.75h55.987z" />
    </svg>
  );
}

function GisIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5a2.5 2.5 0 1 1 0-5 2.5 2.5 0 0 1 0 5z" />
    </svg>
  );
}

function AvitoIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M21.41 11.58l-9-9C12.05 2.22 11.55 2 11 2H4c-1.1 0-2 .9-2 2v7c0 .55.22 1.05.59 1.42l9 9c.36.36.86.58 1.41.58s1.05-.22 1.41-.59l7-7c.37-.36.59-.86.59-1.41 0-.55-.23-1.06-.59-1.42zM5.5 7C4.67 7 4 6.33 4 5.5S4.67 4 5.5 4 7 4.67 7 5.5 6.33 7 5.5 7z" />
    </svg>
  );
}

function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M12.545 10.239v3.821h5.445c-.712 2.315-2.647 3.972-5.445 3.972a6.033 6.033 0 1 1 0-12.064c1.498 0 2.866.549 3.921 1.453l2.814-2.814A9.969 9.969 0 0 0 12.545 2C7.021 2 2.543 6.477 2.543 12s4.478 10 10.002 10c8.396 0 10.249-7.85 9.426-11.748l-9.426-.013z" />
    </svg>
  );
}

const icons: Record<string, (props: { className?: string }) => JSX.Element> = {
  telegram: TelegramIcon,
  vk: VKIcon,
  yandex_business: YandexIcon,
  '2gis': GisIcon,
  avito: AvitoIcon,
  google: GoogleIcon,
};

export function PlatformIcon({ platform, className }: { platform: string; className?: string }) {
  const Icon = icons[platform];
  if (!Icon) return null;
  return <Icon className={className} />;
}
