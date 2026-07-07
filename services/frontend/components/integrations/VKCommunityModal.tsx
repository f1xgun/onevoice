'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { useQueryClient } from '@tanstack/react-query';
import { Loader2 } from 'lucide-react';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';
import { extractApiErrorCode, useMapEmailVerificationError } from '@/lib/resolveErrorMap';

interface Props {
  open: boolean;
  onClose: () => void;
}

// VKCommunityModal is the VK connect entry point. It offers two paths:
//
//   1. Primary — "Authorize with VK": GETs the business-scoped VK auth URL
//      and does a top-level redirect to oauth.vk.com. The browser must land
//      on VK directly so the CSRF cookie issued alongside the auth URL rides
//      the redirect and is present on the callback. After VK returns the
//      user token, the backend redirects to ?vk_step=select_community and the
//      page opens VKCommunityPickerModal.
//
//   2. Fallback — "paste a token manually": collapsed behind a <details>.
//      Collects a community access token pasted from the community admin
//      panel. We deliberately keep this path because VK ID for Business apps
//      issue community tokens with a method whitelist that excludes
//      wall.createComment and wall.delete, regardless of scope. The
//      admin-panel-generated token is the only kind that actually supports
//      the review-reply flow, and it always works even when VK OAuth is
//      unconfigured on the deployment.
//
//   Where the user gets the pasted key (instructions inline below):
//     1. VK community → Управление → Работа с API → Ключи доступа
//     2. «Создать ключ», БЕЗ привязки к приложению
//     3. Отметить «Стена», «Сообщения сообщества», «Управление сообществом»
//     4. Скопировать выданный ключ и вставить ниже.
export function VKCommunityModal({ open, onClose }: Props) {
  const tVk = useTranslations('integrations.vkCommunity');
  const tIntegrations = useTranslations('integrations');
  const qc = useQueryClient();
  const [token, setToken] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [authorizing, setAuthorizing] = useState(false);
  const [pasteOpen, setPasteOpen] = useState(false);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const mapVerifyError = useMapEmailVerificationError();

  function handleClose() {
    setToken('');
    setSubmitting(false);
    setAuthorizing(false);
    setPasteOpen(false);
    onClose();
  }

  async function handleAuthorize() {
    if (!activeBusinessId) return;
    const authUrlPath = INTEGRATION_ENDPOINTS.vk?.authUrl;
    if (!authUrlPath) return;
    setAuthorizing(true);
    try {
      const { data } = await bizApi(activeBusinessId).get<{ url: string }>(authUrlPath);
      window.location.href = data.url;
    } catch {
      toast.error(tIntegrations('page.vkAuthFailed'));
      setAuthorizing(false);
      setPasteOpen(true);
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed || !activeBusinessId) return;
    const connectPath = INTEGRATION_ENDPOINTS.vk?.connect;
    if (!connectPath) return;
    setSubmitting(true);
    try {
      const { data } = await bizApi(activeBusinessId).post<{ id: string; externalId: string }>(
        connectPath,
        { access_token: trimmed }
      );
      toast.success(tVk('connected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      setToken('');
      setSubmitting(false);
      onClose();
      return data;
    } catch (err: unknown) {
      const msg = mapVerifyError(err) ?? extractApiErrorCode(err) ?? tVk('connectFailed');
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

        <div className="space-y-4">
          <p className="text-sm text-ink-mid">{tVk('authorizeIntro')}</p>
          <Button type="button" onClick={handleAuthorize} disabled={authorizing} className="w-full">
            {authorizing && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {authorizing ? tVk('authorizing') : tVk('authorize')}
          </Button>

          <details
            className="rounded-md border border-line-soft bg-paper-sunken"
            open={pasteOpen}
            onToggle={(e) => setPasteOpen((e.currentTarget as HTMLDetailsElement).open)}
          >
            <summary className="cursor-pointer px-4 py-3 text-sm text-ink-mid">
              {tVk('pasteFallback')}
            </summary>

            <form onSubmit={handleSubmit} className="space-y-4 px-4 pb-4">
              <div className="space-y-2 rounded-md border border-line-soft bg-paper px-4 py-3 text-sm text-ink-mid">
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

              <Button type="submit" disabled={submitting || !token.trim()} className="w-full">
                {submitting && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                {submitting ? tVk('connecting') : tVk('connect')}
              </Button>
            </form>
          </details>
        </div>

        <div className="flex pt-2">
          <Button
            type="button"
            variant="outline"
            onClick={handleClose}
            className="flex-1"
            disabled={submitting || authorizing}
          >
            {tVk('cancel')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
