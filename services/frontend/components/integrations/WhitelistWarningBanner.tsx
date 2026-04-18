'use client';

import { useState } from 'react';
import { AlertTriangle, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useProjectsQuery } from '@/hooks/useProjects';
import { PLATFORM_FULL_LABELS } from '@/lib/platforms';
import { toolsForPlatform } from '@/lib/tools-catalogue';
import type { Project } from '@/types/project';

interface Props {
  /** The most-recently-registered integration for this business. */
  integrationId: string;
  businessId: string;
  platform: string;
}

const DISMISS_KEY_PREFIX = 'projects:whitelistWarning:';

export function WhitelistWarningBanner({ integrationId, businessId, platform }: Props) {
  const dismissKey = `${DISMISS_KEY_PREFIX}${businessId}:${integrationId}`;
  const [dismissed, setDismissed] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    try {
      return window.localStorage.getItem(dismissKey) === '1';
    } catch {
      return false;
    }
  });
  const { data: projects } = useProjectsQuery();

  if (dismissed || !projects) return null;

  const newTools = toolsForPlatform(platform);
  if (newTools.length === 0) return null;

  const excluded = (projects ?? [])
    .filter(
      (p: Project) =>
        p.whitelistMode === 'explicit' && !newTools.every((t) => p.allowedTools.includes(t))
    )
    .map((p: Project) => p.name);

  if (excluded.length === 0) return null;

  const platformLabel = PLATFORM_FULL_LABELS[platform] ?? platform;
  const names = excluded.join(', ');

  function onDismiss() {
    try {
      window.localStorage.setItem(dismissKey, '1');
    } catch {
      /* ignore quota errors */
    }
    setDismissed(true);
  }

  return (
    <div className="mb-4 rounded-md border border-yellow-200 bg-yellow-50 p-4 text-sm text-yellow-900 dark:border-yellow-900 dark:bg-yellow-950 dark:text-yellow-100">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="flex-1">
          <p className="font-medium">Новая интеграция не попадёт в whitelist проектов</p>
          <p className="mt-1">
            Эти проекты используют явный whitelist и не включают инструменты {platformLabel}:{' '}
            {names}. Откройте проект, чтобы добавить инструменты.
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onDismiss} aria-label="Закрыть предупреждение">
          <X className="h-4 w-4" />
          <span className="sr-only">Понятно</span>
        </Button>
      </div>
    </div>
  );
}
