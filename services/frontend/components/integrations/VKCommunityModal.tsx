'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import { Loader2 } from 'lucide-react';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { useBusinessStore } from '@/lib/stores/business';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogDescription,
  DialogHeader,
  DialogFooter,
  DialogTitle,
} from '@/components/design-system/AppDialog';

interface Props {
  open: boolean;
  onClose: () => void;
}

// VKCommunityModal keeps the connection flow to one familiar action: sign in
// on VK and choose a community. Credentials and protocol details never enter
// the OneVoice UI.
export function VKCommunityModal({ open, onClose }: Props) {
  const tVk = useTranslations('integrations.vkCommunity');
  const [authorizing, setAuthorizing] = useState(false);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  function handleClose() {
    setAuthorizing(false);
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
      toast.error(tVk('connectFailed'));
      setAuthorizing(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent>
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle>{tVk('title')}</DialogTitle>
          <DialogDescription>{tVk('authorizeIntro')}</DialogDescription>
        </DialogHeader>

        <div className="break-words">
          <Button type="button" onClick={handleAuthorize} disabled={authorizing} className="w-full">
            {authorizing && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {authorizing ? tVk('authorizing') : tVk('authorize')}
          </Button>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={handleClose}
            className="flex-1"
            disabled={authorizing}
          >
            {tVk('cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
