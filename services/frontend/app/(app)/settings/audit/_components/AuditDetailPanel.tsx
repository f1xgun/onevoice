'use client';

import { useTranslations } from 'next-intl';
import type { AuditLogDTO } from '../_lib/types';
import { actionToI18nKey } from '../_lib/actionLabels';
import { isKnownResource } from '../_lib/resourceLabels';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/design-system/AppSheet';
import { DEFAULT_LOCALE, localeToIntlTag } from '@/lib/i18n/locales';

const JSON_INDENT = 2;
const LOCALE = localeToIntlTag(DEFAULT_LOCALE);

interface Props {
  item: AuditLogDTO | null;
  onClose: () => void;
}

type AuditFiltersTranslator = ReturnType<typeof useTranslations<'audit.filters'>>;

function resolveActor(item: AuditLogDTO, tFilters: AuditFiltersTranslator): string {
  if (item.actor_display_name) return item.actor_display_name;
  if (item.actor_email) return item.actor_email;
  const d = item.details as Record<string, unknown> | null;
  const attempted =
    d && typeof d === 'object' && typeof d.attempted_email === 'string' ? d.attempted_email : '—';
  return tFilters('actorUnknown', { email: attempted });
}

// AuditDetailPanel slides in from the right when a table row is clicked.
// Reuses the project's existing <Sheet side="right"> primitive so the
// motion + overlay match the Linen design system (the page intentionally
// adds NO new chrome). Closing emits onClose so the parent can clear
// `selected` and avoid a controlled/uncontrolled mismatch.
export function AuditDetailPanel({ item, onClose }: Props) {
  const tPanel = useTranslations('audit.panel');
  const tFilters = useTranslations('audit.filters');
  const tActions = useTranslations();
  const tResources = useTranslations('audit.resources');

  if (!item) return null;

  return (
    <Sheet
      open={!!item}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{tActions(actionToI18nKey(item.action))}</SheetTitle>
          <SheetDescription>{new Date(item.created_at).toLocaleString(LOCALE)}</SheetDescription>
        </SheetHeader>
        <div className="mt-4 space-y-4 text-sm">
          <div>
            <div className="text-ink-soft">{tPanel('actor')}</div>
            <div data-testid="panel-actor" className="text-ink">
              {resolveActor(item, tFilters)}
            </div>
          </div>
          <div>
            <div className="text-ink-soft">{tPanel('resource')}</div>
            <div className="text-ink">
              {isKnownResource(item.resource) ? tResources(item.resource) : item.resource}
            </div>
          </div>
          <div>
            <div className="mb-1 text-ink-soft">{tPanel('rawDetails')}</div>
            <pre
              data-testid="panel-raw-json"
              className="overflow-auto rounded-md bg-paper-sunken p-2 font-mono text-xs text-ink"
            >
              {JSON.stringify(item.details, null, JSON_INDENT)}
            </pre>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
