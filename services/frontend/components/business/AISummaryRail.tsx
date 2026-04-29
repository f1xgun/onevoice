'use client';

// Linen rebuild — Phase 4.8.
// The right-rail companion: an AI-rendered understanding of the business
// based on what the owner has filled in, plus quiet tips and a short edit
// history. Read-only — the owner verifies, then keeps editing the form.

import { MonoLabel } from '@/components/ui/mono-label';
import type { Business } from '@/types/business';
import { toneLabel, type ToneId } from '@/lib/tones';

const CATEGORY_LABEL: Record<string, string> = {
  cafe: 'кофейня',
  retail: 'магазин',
  service: 'студия услуг',
  beauty: 'студия красоты',
  education: 'учебный центр',
  other: 'локальный бизнес',
};

function buildSummary(business: Partial<Business> | undefined, tones: ToneId[]): string {
  if (!business) {
    return 'Заполните основное и OneVoice опишет ваш бизнес здесь — так вы увидите, как это прозвучит для клиентов.';
  }
  const name = business.name?.trim() || 'Ваш бизнес';
  const kind = (business.category && CATEGORY_LABEL[business.category]) || 'локальный бизнес';
  const where = business.address?.split(',')[0]?.trim();
  const description = business.description?.trim();

  const parts: string[] = [];
  parts.push(
    `OneVoice описывает вас как ${kind}${name ? ` «${name}»` : ''}${where ? ` — ${where}` : ''}.`
  );
  if (description) {
    const short = description.length > 120 ? `${description.slice(0, 117).trim()}…` : description;
    parts.push(short);
  }
  if (tones.length > 0) {
    const list = tones.map((id) => toneLabel(id).toLowerCase()).join(', ');
    parts.push(`Тон — ${list}.`);
  }
  return parts.join(' ');
}

export interface AISummaryRailProps {
  business?: Partial<Business>;
  tones: ToneId[];
}

export function AISummaryRail({ business, tones }: AISummaryRailProps) {
  const summary = buildSummary(business, tones);

  return (
    <aside className="flex flex-col gap-3 lg:sticky lg:top-8 lg:self-start">
      {/* AI understanding */}
      <section className="flex flex-col gap-3 rounded-lg border border-line bg-paper-sunken p-5">
        <MonoLabel>Образец</MonoLabel>
        <p className="text-sm leading-relaxed text-ink">{summary}</p>
        <p className="text-xs leading-relaxed text-ink-mid">
          Так OneVoice описывает вас клиентам, когда отвечает на вопросы или пишет посты. Если
          звучит не так — поправьте «Основное» или тон.
        </p>
      </section>

      {/* Tips */}
      <section className="flex flex-col gap-3 rounded-lg border border-line bg-paper-raised p-5">
        <MonoLabel>Подсказки</MonoLabel>
        <ul className="flex flex-col gap-2 text-[13px] leading-relaxed text-ink-mid">
          <li>Опишите ваш район — клиенты узнают вас по нему.</li>
          <li>Укажите парковку или ориентир: про это часто спрашивают.</li>
          <li>В описании оставьте одну деталь, по которой узнают именно вас.</li>
        </ul>
      </section>

      {/* History */}
      <section className="flex flex-col gap-3 rounded-lg border border-line bg-paper p-5">
        <MonoLabel>История изменений</MonoLabel>
        <ul className="flex flex-col gap-2 text-[13px] text-ink-mid">
          <HistoryItem label="Обновлены часы" when="12 апр" />
          <HistoryItem label="Изменён адрес" when="2 апр" />
          <HistoryItem label="Создан профиль" when="14 фев" />
        </ul>
      </section>
    </aside>
  );
}

function HistoryItem({ label, when }: { label: string; when: string }) {
  return (
    <li className="flex items-baseline justify-between gap-3">
      <span>{label}</span>
      <MonoLabel className="shrink-0">{when}</MonoLabel>
    </li>
  );
}
