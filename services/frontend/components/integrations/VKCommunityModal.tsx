'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useQueryClient } from '@tanstack/react-query';
import { Loader2 } from 'lucide-react';
import { api } from '@/lib/api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';

interface Props {
  open: boolean;
  onClose: () => void;
}

// VKCommunityModal collects a community access token pasted from the
// community admin panel. We deliberately do NOT route through OAuth here:
// VK ID for Business apps issue community tokens with a method whitelist
// that excludes wall.createComment and wall.delete, regardless of scope.
// The admin-panel-generated token is the only kind that actually supports
// the review-reply flow.
//
// Where the user gets it (instructions inline below):
//   1. VK community → Управление → Работа с API → Ключи доступа
//   2. «Создать ключ», БЕЗ привязки к приложению
//   3. Отметить «Стена», «Сообщения сообщества», «Управление сообществом»
//   4. Скопировать выданный ключ и вставить ниже.
export function VKCommunityModal({ open, onClose }: Props) {
  const tVk = useTranslations('integrations.vkCommunity');
  const qc = useQueryClient();
  const [token, setToken] = useState('');
  const [submitting, setSubmitting] = useState(false);

  function handleClose() {
    setToken('');
    setSubmitting(false);
    onClose();
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      const { data } = await api.post<{ id: string; externalId: string }>(
        '/integrations/vk/connect',
        { access_token: trimmed }
      );
      toast.success(tVk('connected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.INTEGRATIONS });
      // Reset local state then call parent close to keep the modal logic clean.
      setToken('');
      setSubmitting(false);
      onClose();
      // Note: returning data avoids an unused-var lint hint when adding
      // post-connect UX later (e.g., showing the resolved community name).
      return data;
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        tVk('connectFailed');
      toast.error(msg);
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{tVk('title')}</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2 rounded-md border border-line-soft bg-paper-sunken px-4 py-3 text-sm text-ink-mid">
            <p className="font-medium text-ink">{tVk('tokenIntro')}</p>
            <ol className="ml-4 list-decimal space-y-1">
              <li>{tVk('step1')}</li>
              <li>
                {tVk('step2Prefix')}
                <span className="font-medium text-ink">{tVk('step2Highlight')}</span>
                {tVk('step2Suffix')}
              </li>
              <li>
                <span className="font-medium text-ink">{tVk('warningHighlight')}</span>
                {tVk('warningSuffix')}
              </li>
              <li>
                {tVk('permissionsPrefix')}
                <span className="font-mono text-xs">{tVk('permWall')}</span>
                {tVk('permSeparator')}
                <span className="font-mono text-xs">{tVk('permMessages')}</span>
                {tVk('permSeparator')}
                <span className="font-mono text-xs">{tVk('permManage')}</span>
                {tVk('step2Suffix')}
              </li>
              <li>{tVk('step5')}</li>
            </ol>
          </div>

          <div className="space-y-2">
            <label className="text-sm text-ink-mid" htmlFor="vk-community-token">
              {tVk('tokenLabel')}
            </label>
            <Textarea
              id="vk-community-token"
              autoFocus
              spellCheck={false}
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={tVk('tokenPlaceholder')}
              rows={3}
              className="font-mono text-xs"
              disabled={submitting}
            />
            <p className="text-xs text-ink-soft">{tVk('tokenFooter')}</p>
          </div>

          <div className="flex gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              className="flex-1"
              disabled={submitting}
            >
              {tVk('cancel')}
            </Button>
            <Button type="submit" disabled={submitting || !token.trim()} className="flex-1">
              {submitting && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              {submitting ? tVk('connecting') : tVk('connect')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
