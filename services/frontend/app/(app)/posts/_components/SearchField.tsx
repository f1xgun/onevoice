// app/(app)/posts/_components/SearchField.tsx — Linen ⌘K search
// affordance for the posts page filter bar.
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12.
import { Search } from 'lucide-react';
import { useTranslations } from 'next-intl';

import { Input } from '@/components/ui/input';

export function SearchField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const tPosts = useTranslations('posts');
  return (
    <label className="relative inline-flex h-8 w-[260px] items-center">
      <Search aria-hidden className="pointer-events-none absolute left-3 size-3.5 text-ink-soft" />
      <Input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Поиск по содержимому…"
        className="h-8 bg-paper-sunken pl-9 pr-12 text-[13px]"
      />
      <span
        aria-hidden
        className="pointer-events-none absolute right-2 rounded border border-line-soft bg-paper px-1.5 py-0.5 font-mono text-[10px] text-ink-soft"
      >
        {tPosts('shortcut')}
      </span>
    </label>
  );
}
