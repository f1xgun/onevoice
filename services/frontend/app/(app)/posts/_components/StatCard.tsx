// app/(app)/posts/_components/StatCard.tsx — Linen stat card for the
// posts page top-strip (4-card geometry).
//
// Extracted from posts/page.tsx as part of Phase 19 / 19-12 to keep the
// page shell under the SC-01 LOC ceiling.
import { MonoLabel } from '@/components/ui/mono-label';

export function StatCard({
  label,
  value,
  hint,
  tone = 'neutral',
}: {
  label: string;
  value: string;
  hint: string;
  tone?: 'neutral' | 'danger' | 'muted';
}) {
  const labelTone = tone === 'danger' ? 'ochre' : 'soft';
  return (
    <div className="rounded-md border border-line bg-paper-raised px-5 py-4">
      <MonoLabel
        tone={labelTone}
        className={tone === 'danger' ? 'text-[var(--ov-danger)]' : undefined}
      >
        {label}
      </MonoLabel>
      <div
        className={
          'mt-1 text-[28px] font-medium tracking-[-0.015em] ' +
          (tone === 'muted' ? 'text-ink-soft' : 'text-ink')
        }
      >
        {value}
      </div>
      <div className="mt-0.5 text-xs text-ink-soft">{hint}</div>
    </div>
  );
}
