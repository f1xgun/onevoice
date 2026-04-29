// app/not-found.tsx — OneVoice (Linen) global 404
//
// Next 13+ convention: a top-level `not-found.tsx` in the app router
// catches any URL that doesn't resolve to a route. The layout above
// (`app/layout.tsx`) renders the providers + html shell, so this file
// only paints the body content.
//
// Mirrors the "404 — страницы нет" panel from
// design_handoff_onevoice 2/mocks/mock-states.jsx (ErrorStatesPage):
// calm paper background, mono "ERROR · 404" caption, big graphite
// headline ("Такого здесь нет"), short paragraph, single primary
// button back to /chat. Brand voice — no "Oops!", no emoji.

import Link from 'next/link';
import { Button } from '@/components/ui/button';
import { MonoLabel } from '@/components/ui/mono-label';

export default function NotFound() {
  return (
    <main className="flex min-h-screen w-full items-center justify-center bg-paper px-6 py-16">
      <div className="flex w-full max-w-[480px] flex-col items-center gap-5 rounded-lg border border-line bg-paper-raised px-8 py-20 text-center shadow-ov-1">
        <MonoLabel className="text-ink-soft">ERROR · 404</MonoLabel>
        <div
          aria-hidden="true"
          className="font-mono text-[56px] leading-none tracking-[-0.02em] text-ink-faint"
        >
          404
        </div>
        <div>
          <h1 className="text-[22px] font-medium leading-tight tracking-[-0.01em] text-ink">
            Такого здесь нет
          </h1>
          <p className="mt-2 text-sm leading-relaxed text-ink-mid">
            Возможно, ссылка устарела или раздел был перемещён. Попробуйте начать заново — главная всегда на месте.
          </p>
        </div>
        <Button asChild variant="primary" size="md">
          <Link href="/chat">На главную</Link>
        </Button>
      </div>
    </main>
  );
}
