'use client';

import { useEffect, useRef, useState, useTransition } from 'react';
import { useRouter } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useTheme } from '@/components/design-system/ThemeProvider';
import type { Theme } from '@/lib/theme';

export function useThemeSwitcher() {
  const theme = useTheme();
  const router = useRouter();
  const t = useTranslations('theme');
  const [isPending, startTransition] = useTransition();
  const [saving, setSaving] = useState(false);
  const inFlight = useRef(false);
  const [selection, setSelection] = useState<{ source: Theme; value: Theme }>();
  const active = selection?.source === theme ? selection.value : theme;
  useEffect(() => setSelection(undefined), [theme]);

  async function select(next: Theme) {
    if (inFlight.current || isPending || next === active) return;
    const previous = active;
    inFlight.current = true;
    setSaving(true);
    setSelection({ source: theme, value: next });
    try {
      const response = await fetch('/api/theme', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ theme: next }),
      });
      if (!response.ok) throw new Error('theme persistence failed');
      startTransition(() => router.refresh());
    } catch {
      setSelection({ source: theme, value: previous });
      toast.error(t('saveFailed'));
    } finally {
      inFlight.current = false;
      setSaving(false);
    }
  }

  return { active, disabled: saving || isPending, select };
}
