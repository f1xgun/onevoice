// Phase 22-02 — Surface A/B/C shared page header. Composes the page
// title (32px/500 per UI-SPEC §A typography scale) and the EffectiveTag
// «Действует с …» row. Mounted by the three (public)/legal/*/page.tsx
// async server components and the contact page.

import { EffectiveTag } from './EffectiveTag';

interface LegalPageHeaderProps {
  title: string;
  version: string;
  effectiveFrom: string;
}

export function LegalPageHeader({ title, version, effectiveFrom }: LegalPageHeaderProps) {
  return (
    <header className="mb-12">
      <h1 className="text-[32px] font-medium leading-[1.2] tracking-[-0.015em] text-[var(--ov-ink)]">
        {title}
      </h1>
      <EffectiveTag version={version} effectiveFrom={effectiveFrom} />
    </header>
  );
}
